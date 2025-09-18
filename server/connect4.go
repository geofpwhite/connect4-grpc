package server

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log"
	"math/rand/v2"
	"net"
	"os"
	"sync"

	"github.com/geofpwhite/connect4-grpc/pb"
	"google.golang.org/grpc"
)

type field [8][8]pb.Team

type game struct {
	turn                    pb.Team
	state                   field
	mut                     *sync.RWMutex
	red                     bool // true if player1 is connect
	yellow                  bool // true if player2 is connected
	redWins, yellowWins     int
	redStream, yellowStream grpc.BidiStreamingServer[pb.Input, pb.State]
}

type connect4Server struct {
	games map[int32]*game
	pb.UnimplementedConnect4Server
}

func newServer() *connect4Server {
	return &connect4Server{
		games: make(map[int32]*game),
	}
}

func (cs *connect4Server) update(id int32, s *pb.State) error {
	g, exists := cs.games[id]
	if !exists {
		return errors.New("game does not exist")
	}
	wg := &sync.WaitGroup{}
	wg.Add(2)
	go func() {
		if g.yellowStream != nil {
			if err := g.yellowStream.Send(s); err != nil {
				fmt.Fprintln(os.Stderr, "yellow stream can't send")
			}
		}
		wg.Done()
	}()
	go func() {
		if g.redStream != nil {
			if err := g.redStream.Send(s); err != nil {
				fmt.Fprintln(os.Stderr, "red stream can't send")
			}
		}
		wg.Done()
	}()
	wg.Wait()
	return nil
}

func (cs *connect4Server) JoinGame(_ context.Context, id *pb.GameID) (*pb.GameIDAndTeam, error) {
	if game, exists := cs.games[id.GetId()]; exists {
		game.mut.RLock()
		if game.red && game.yellow {
			game.mut.RUnlock()
			return nil, errors.New("game is full")
		}
		red, yellow := game.red, game.yellow
		game.mut.RUnlock()
		game.mut.Lock()
		defer game.mut.Unlock()
		if !red {
			game.red = true
			return &pb.GameIDAndTeam{Id: id.Id, Team: pb.Team_red.Enum()}, nil
		} else if !yellow {
			game.yellow = true
			return &pb.GameIDAndTeam{Id: id.Id, Team: pb.Team_yellow.Enum()}, nil
		}
	}
	return nil, errors.New("game does not exist")
}

func (cs *connect4Server) LeaveGame(_ context.Context, idAndTeam *pb.GameIDAndTeam) (*pb.Empty, error) {
	if game, exists := cs.games[idAndTeam.GetId()]; exists {
		game.mut.Lock()
		if idAndTeam.GetTeam().Enum() == pb.Team_yellow.Enum() {
			game.yellowStream = nil
			game.yellow = false
		} else {
			game.redStream = nil
			game.red = false
		}
		game.mut.Unlock()
		if !game.yellow && !game.red {
			delete(cs.games, idAndTeam.GetId())
		}
	}
	return &pb.Empty{}, nil
}

func (cs *connect4Server) CommunicateState(stream grpc.BidiStreamingServer[pb.Input, pb.State]) error {
	input, err := stream.Recv() // client should send first message with column = -1 in order to place stream in game struct field
	if errors.Is(err, io.EOF) {
		return nil
	}
	if err != nil {
		return err
	}
	game, exists := cs.games[input.GetGameId()]
	if !exists {
		return nil
	}
	if *input.GetInputTeam().Enum() == *pb.Team_yellow.Enum() {
		game.mut.Lock()
		game.yellowStream = stream

		game.mut.Unlock()
		defer func() {
			game.mut.Lock()
			game.yellowStream = nil
			game.yellow = false
			game.mut.Unlock()
		}()
	} else {
		game.mut.Lock()
		game.redStream = stream
		game.mut.Unlock()
		defer func() {
			game.mut.Lock()
			game.redStream = nil
			game.red = false
			game.mut.Unlock()
		}()
	}

	for {
		input, err := stream.Recv()
		if errors.Is(err, io.EOF) {
			return nil
		}
		fmt.Println(input)
		if err != nil {
			return err
		}
		game, exists := cs.games[input.GetGameId()]
		if !exists {
			return nil
		}
		game.modifyState(input.GetColumn(), input.GetInputTeam())
		s := &pb.State{}
		s.Turn = &game.turn
		field := pb.Field{Rows: []*pb.Row{}}

		for i := range game.state {
			field.Rows = append(field.Rows, &pb.Row{Values: make([]pb.Team, 8)})
			for j := range game.state {
				field.Rows[i].Values[j] = game.state[i][j]
			}
		}
		s.Field = &field
		err = cs.update(input.GetGameId(), s)
		if err != nil {
			return err
		}
	}
}

func (cs *connect4Server) NewGame(_ context.Context, _ *pb.Empty) (*pb.GameIDAndTeam, error) {
	id := rand.Int32() //nolint: gosec // it's just the game id
	_, exists := cs.games[id]
	for exists {
		id = rand.Int32() //nolint: gosec // it's just the game id
		_, exists = cs.games[id]
	}
	cs.games[id] = &game{
		mut:   &sync.RWMutex{},
		state: field{},
		red:   true,
		turn:  1,
	}
	return &pb.GameIDAndTeam{Id: &id, Team: pb.Team_red.Enum()}, nil
}

func (g *game) modifyState(column int32, inputTeam pb.Team) {
	g.mut.RLock()
	if g.state[7][int(column-1)] != 0 || inputTeam != g.turn {
		g.mut.RUnlock()
		return
	}
	g.mut.RUnlock()
	g.mut.Lock()
	g.state[7][column-1] = inputTeam
	g.state = fall(g.state, column-1)
	g.turn = (g.turn % 2) + 1
	g.mut.Unlock()
	g.mut.RLock()
	ended := scan(g.state, inputTeam)

	g.mut.RUnlock()

	if !ended {
		return
	}
	g.mut.Lock()
	g.state = field{}
	if inputTeam == *pb.Team_yellow.Enum() {
		g.yellowWins++
	} else {
		g.redWins++
	}
	g.turn = pb.Team_red
	g.mut.Unlock()

}

func fall(state field, column int32) field {
	for i := range 8 {
		if state[i][column] != 0 {
			for j := i; j > 0 && state[j-1][column] == 0; j-- {
				state[j-1][column], state[j][column] = state[j][column], 0
			}
		}
	}

	return state
}

type (
	coords struct{ x, y int }
	qNode  struct {
		directionStreak int
		streak          int
		coords          coords
	}
)

func scan(state field, team pb.Team) bool {
	visited := make(map[coords]map[int]int)
	queue := make([]qNode, 0)
	for i := range state {
		if state[0][i] == team {
			queue = append(queue, qNode{-1, 1, coords{0, i}})
		}
	}
	for len(queue) > 0 {
		cur := queue[0]
		m, ok := visited[cur.coords]
		if !ok {
			visited[cur.coords] = make(map[int]int)
			m = visited[cur.coords]
		}
		m[cur.directionStreak] = cur.streak
		queue = queue[1:]
		fmt.Println(cur)
		if cur.streak >= 4 {
			return true
		}
		toCheck := []coords{
			{cur.coords.x, cur.coords.y + 1},
			{cur.coords.x + 1, cur.coords.y + 1},
			{cur.coords.x + 1, cur.coords.y},
			{cur.coords.x + 1, cur.coords.y - 1},
		}
		for i, coords := range toCheck {
			if coords.x >= 0 && coords.x < 8 && coords.y >= 0 &&
				coords.y < 8 && state[coords.x][coords.y] == team && visited[coords][i] < cur.streak+1 {
				qn := qNode{i, 2, coords}
				if cur.directionStreak == i {
					qn = qNode{cur.directionStreak, cur.streak + 1, coords}
				}
				queue = append(queue, qn)
			}
		}
	}
	return false
}

func Serve() {
	lis, err := net.Listen("tcp", "0.0.0.0:50051")
	if err != nil {
		log.Fatalf("failed to listen: %v", err)
	}
	var opts []grpc.ServerOption
	grpcServer := grpc.NewServer(opts...)
	pb.RegisterConnect4Server(grpcServer, newServer())
	if err := grpcServer.Serve(lis); err != nil {
		log.Fatal("server failed")
	}
}
