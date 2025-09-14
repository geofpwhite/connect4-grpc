package clients

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"image"
	"image/color"
	"image/draw"
	"io"
	"math"
	"strconv"

	"fortio.org/log"
	"fortio.org/terminal/ansipixels"
	"fortio.org/terminal/ansipixels/tcolor"
	"github.com/geofpwhite/connect4-grpc/pb"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
)

type game struct {
	id    int32
	team  pb.Team
	state [8][8]pb.Team
}

func Main() { //nolint: funlen,gocognit,gocyclo,maintidx //this is the main function it's gonna get a bit big
	ap := ansipixels.NewAnsiPixels(60)
	err := ap.Open()
	if err != nil {
		panic("")
	}
	newGame := flag.Bool("new", false, "Create a new game to play with a friend")
	joinID := flag.Int("join-id", -1, "id of game to join")
	flag.Parse()

	conn, err := grpc.NewClient("64.227.12.170:50051", grpc.WithTransportCredentials(insecure.NewCredentials()))
	// conn, err := grpc.NewClient("localhost:50051", grpc.WithTransportCredentials(insecure.NewCredentials()))
	// connected := false
	if err != nil {
		panic(err)
	}
	defer conn.Close()
	client := pb.NewConnect4Client(conn)
	g := &game{}
	if *newGame {
		id, initErr := client.NewGame(context.Background(), &pb.Empty{}, grpc.EmptyCallOption{})
		g.id = id.GetId()
		g.team = id.GetTeam()
		if initErr != nil {
			panic(fmt.Sprintf("issue starting game: %s", initErr))
		}
	} else {
		id := int32(*joinID) //nolint:gosec //panic is fine if they give number that overflows
		idAndTeam, joinErr := client.JoinGame(context.Background(), &pb.GameID{Id: &id})
		if joinErr != nil {
			panic("no game with that id")
		}
		g.id = id
		g.team = idAndTeam.GetTeam()
	}
	stream, err := client.CommunicateState(context.Background())
	if err != nil {
		panic("Error starting stream")
	}
	stateChan := make(chan ([8][8]pb.Team))
	inputChan := make(chan int)
	startColumn := int32(-1)
	inputObj := &pb.Input{GameId: &g.id, InputTeam: &g.team, Column: &startColumn}
	err = stream.Send(inputObj)
	if err != nil {
		log.Infof("error sending initial connection message")
	}
	defer func() {
		if _, leaveErr := client.LeaveGame(context.Background(), &pb.GameIDAndTeam{Id: &g.id, Team: &g.team}); leaveErr != nil {
			log.FErrf("error leaving")
		}
	}()

	go func() {
		stateReceived := [8][8]pb.Team{}
		for {
			in, streamErr := stream.Recv()
			if errors.Is(streamErr, io.EOF) {
				close(stateChan)
				return
			}
			if streamErr != nil {
				continue
			}
			for i, row := range in.GetField().GetRows() {
				copy(stateReceived[i][:], row.GetValues())
			}
			stateChan <- stateReceived
		}
	}()
	go func() {
		for input := range inputChan {
			i32 := int32(input) //nolint:gosec //input will never be greater than 8
			inputObj.Column = &i32
			sendErr := stream.Send(inputObj)
			if sendErr != nil {
				// panic(err)
				//
				continue
			}
		}
	}()

	defer func() {
		ap.MouseClickOff()
		ap.MouseTrackingOff()
		ap.ShowCursor()
		ap.Restore()
	}()

	ap.TrueColor = true
	ap.MouseClickOn()
	ap.MouseTrackingOn()
	ap.ClearScreen()
	ap.HideCursor()
	img := image.NewRGBA(image.Rect(0, 0, ap.W, ap.H*2))
	draw.Draw(img, img.Rect, image.NewUniform(color.Black), image.Point{}, draw.Over)
	frame := 0
	highlightedColumn := -1
	ap.OnResize = func() error {
		img = image.NewRGBA(image.Rect(0, 0, ap.W, ap.H*2))
		return nil
	}
	err = ap.FPSTicks(context.Background(), func(context.Context) bool {
		frame = (frame + 1) % 60
		select {
		case state := <-stateChan:
			//
			ap.ClearScreen()
			img = image.NewRGBA(image.Rect(0, 0, ap.W, ap.H*2))
			draw.Draw(img, img.Rect, image.NewUniform(color.Black), image.Point{}, draw.Over)

			g.state = state
			// for i, row := range g.state {
			// 	for j, value := range row {
			// 		clr := color.RGBA{}
			// 		switch value { //nolint: exhaustive // keep it black if empty
			// 		case 1:
			// 			clr = color.RGBA{255, 0, 0, 255}
			// 		case 2:
			// 			clr = color.RGBA{0, 0, 255, 255}
			// 		}
			// 		x := (ap.W / 10) * (1 + j)
			// 		y := ((ap.H * 2) - ((ap.H / 5) * (1 + i)))
			// 		xBound := x + (ap.W / 10)
			// 		yBound := ((ap.H * 2) - ((ap.H / 5) * (2 + i)))

			// 		radius := ap.W / 14
			// 		DrawDisc((x+xBound)/2, (y+yBound)/2, clr, img, radius)
			// 	}
			// }
		default:
		}
		for i := range g.state {
			x := (ap.W / 10) * (1 + i)
			xBound := x + (ap.W / 10)
			clr := color.RGBA{0, 0, 0, 50}
			if ap.Mx >= x && ap.Mx < xBound && highlightedColumn != i {
				clr = color.RGBA{255, 255, 255, 50}
				highlightedColumn = i
			}
			draw.Draw(img, image.Rect(x, ap.H/5, xBound, ap.H*9/5), &image.Uniform{clr}, image.Point{}, draw.Over)
		}
		ap.Draw216ColorImage(0, 0, img)
		for i, row := range g.state {
			for j, value := range row {
				x := (ap.W / 10) * (1 + j)
				clr := tcolor.RGBColor{}
				y := ((ap.H) - ((ap.H / 10) * (1 + i)))
				xBound := x + (ap.W / 10)
				yBound := y - ap.H/10
				switch value {
				case 1:
					clr = tcolor.RGBColor{255, 0, 0}
				case 2:
					clr = tcolor.RGBColor{255, 255, 0}
				case 0:
					continue
				}

				radius := min((xBound-x)/2, (y - yBound))
				ap.DiscSRGB((x+xBound)/2, yBound, radius, clr, clr, .1)
			}
		}
		if len(ap.Data) > 0 && ap.Data[0] == 'q' {
			return false
		}
		if ap.LeftClick() || ap.LeftDrag() {
			column := ap.Mx / (ap.W / 10)
			ap.WriteAtStr(1, ap.H-1, strconv.Itoa(column))
			if column > 0 && column < 9 {
				inputChan <- column
			}
		}
		// ap.WriteAtStr(1, 1, fmt.Sprintf("%d", frame))
		for i := range 8 {
			ap.DrawRoundBox(ap.W*(1+i)/10, ap.H/10, ap.W/10, ap.H*8/10)
			// for j := range 8 {
			// 	x := (ap.W / 10) * (1 + j)
			// 	y := ((ap.H / 10) * (1 + i))
			// 	xBound := x + (ap.W / 10)
			// 	yBound := y + (ap.H / 10)

			// 	// draw.Draw(img, image.Rect(x, y, xBound, yBound), &image.Uniform{clr}, image.Point{}, draw.Over)
			// 	// ap.DrawSquareBox(x+1, y+1, xBound-x-1, yBound-y-1)
			// }
		}
		ap.WriteAtStr(1, 1, fmt.Sprintf("%d", g.id))
		ap.WriteAtStr(1, 1, fmt.Sprintf("%d%d", tcolor.Black))
		return true
	})
	if err != nil {
		log.FErrf("%e", err)
	}
}

type coords struct{ x, y int }

func DrawDisc(x, y int, clr color.RGBA, img *image.RGBA, radius int) {
	bounds := circleBounds(x, y, img, radius)
	for x, yBounds := range bounds {
		for yValue := yBounds.x; yValue < yBounds.y; yValue++ {
			ansipixels.AddPixel(img, x, yValue, clr)
		}
	}
}

func circleBounds(x, y int, img *image.RGBA, radius int) map[int]*coords {
	bounds := make(map[int]*coords)
	for i := 0.; i < math.Pi; i += math.Pi / (float64(radius)) { // tbd
		ex := .25 * float64(radius) * (math.Cos(i))
		ey := .25 * float64(radius) * (math.Sin(2*math.Pi - i))
		eyUpper := .25 * float64(radius) * (math.Sin(i))
		rx, ry := int(ex)+x, int(ey)+y
		ryUpper := int(eyUpper) + y
		if rx > img.Bounds().Dx() {
			rx = img.Bounds().Dx()
		}
		if ryUpper > img.Bounds().Dy() {
			ryUpper = img.Bounds().Dy()
		}
		bounds[rx] = &coords{ry, ryUpper}
	}
	return bounds
}
