package clients

import (
	"context"
	"flag"
	"image"
	"image/color"
	"image/draw"
	"io"
	"strconv"

	"fortio.org/terminal/ansipixels"
	"github.com/geofpwhite/connect4-grpc/pb"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
)

type game struct {
	id    int32
	team  pb.Team
	state [8][8]pb.Team
}

func Main() {
	ap := ansipixels.NewAnsiPixels(60)
	err := ap.Open()
	if err != nil {
		panic("")
	}
	newGame := flag.Bool("new", false, "Create a new game to play with a friend")
	joinID := flag.Int("join-id", -1, "id of game to join")
	flag.Parse()

	conn, err := grpc.NewClient("localhost:4040", grpc.WithTransportCredentials(insecure.NewCredentials()))
	// connected := false
	if err != nil {
		panic(err)
	}
	defer conn.Close()
	client := pb.NewConnect4Client(conn)
	g := &game{}
	if *newGame {
		id, err := client.NewGame(context.Background(), &pb.Empty{}, grpc.EmptyCallOption{})
		g.id = *id.Id
		g.team = *id.Team
		if err != nil {
			panic("issue starting game")
		}
	} else {
		id := int32(*joinID)
		idAndTeam, err := client.JoinGame(context.Background(), &pb.GameID{Id: &id})
		if err != nil {
			panic("no game with that id")
		}
		g.id = id
		g.team = *idAndTeam.Team
	}
	stream, err := client.CommunicateState(context.Background())
	if err != nil {
		panic("Error starting stream")
	}
	stateChan := make(chan ([8][8]pb.Team))
	inputChan := make(chan int)
	startColumn := int32(-1)
	inputObj := &pb.Input{GameId: &g.id, InputTeam: &g.team, Column: &startColumn}
	//
	stream.Send(inputObj)
	defer client.LeaveGame(context.Background(), &pb.GameIDAndTeam{Id: &g.id, Team: &g.team})

	go func() {
		stateReceived := [8][8]pb.Team{}
		for {
			in, err := stream.Recv()
			if err == io.EOF {
				close(stateChan)
				return
			}
			if err != nil {

				continue
			}
			for i, row := range in.Field.Rows {
				copy(stateReceived[i][:], row.Values)
				for _, value := range row.Values {
					if value == pb.Team_blue {

					}
				}

			}
			stateChan <- stateReceived

		}
	}()
	go func() {
		for input := range inputChan {
			i32 := int32(input)
			inputObj.Column = &i32
			err := stream.Send(inputObj)
			if err != nil {
				// panic(err)
				//
				continue
			}
		}
	}()

	defer func() {
		ap.MouseClickOff()
		ap.ShowCursor()
		ap.Restore()
	}()

	ap.TrueColor = true
	ap.MouseClickOn()
	ap.ClearScreen()
	ap.HideCursor()
	img := image.NewRGBA(image.Rect(0, 0, ap.W, ap.H*2))
	draw.Draw(img, img.Rect, image.NewUniform(color.Black), image.Point{}, draw.Over)
	frame := 0
	ap.FPSTicks(context.Background(), func(ctx context.Context) bool {
		frame = (frame + 1) % 60
		select {
		case state := <-stateChan:
			//
			ap.ClearScreen()
			img := image.NewRGBA(image.Rect(0, 0, ap.W, ap.H*2))
			draw.Draw(img, img.Rect, image.NewUniform(color.Black), image.Point{}, draw.Over)

			g.state = state
			for i, row := range g.state {
				for j, value := range row {
					clr := color.RGBA{}
					switch value {
					case 1:
						clr = color.RGBA{255, 0, 0, 255}
					case 2:
						clr = color.RGBA{0, 0, 255, 255}
					}
					x := (ap.W / 10) * (1 + j)
					y := ((ap.H * 2) - ((ap.H / 5) * (1 + i)))
					xBound := x + (ap.W / 10)
					yBound := ((ap.H * 2) - ((ap.H / 5) * (2 + i)))
					//
					draw.Draw(img, image.Rect(x, y, xBound, yBound), &image.Uniform{clr}, image.Point{}, draw.Over)
				}
			}
			ap.Draw216ColorImage(0, 0, img)
		default:
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

		return true
	})
}
