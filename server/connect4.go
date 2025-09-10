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
	turn                  pb.Team
	state                 field
	mut                   *sync.RWMutex
	red                   bool // true if player1 is connect
	blue                  bool //true if player2 is connected
	redWins, blueWins     int
	redStream, blueStream grpc.BidiStreamingServer[pb.Input, pb.State]
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
	if g, exists := cs.games[id]; exists {
		g.mut.Lock()
		defer g.mut.Unlock()
		// go func() {
		if g.blueStream != nil {

			if err := g.blueStream.Send(s); err != nil {
				fmt.Fprintln(os.Stderr, "blue stream can't send")
			}

		}
		// }()
		// go func() {
		if g.redStream != nil {

			if err := g.redStream.Send(s); err != nil {
				fmt.Fprintln(os.Stderr, "red stream can't send")
			}

		}
		// }()
	}
	return nil
}

// CommunicateState(grpc.BidiStreamingServer[Input, State]) error
// NewGame(context.Context, *Empty) (*GameIDAndTeam, error)
// JoinGame(context.Context, *GameID) (*GameIDAndTeam, error)
// LeaveGame(context.Context, *GameIDAndTeam) (*Empty, error)
func (cs *connect4Server) JoinGame(ctx context.Context, id *pb.GameID) (*pb.GameIDAndTeam, error) {

	if game, exists := cs.games[*id.Id]; exists {

		game.mut.RLock()

		if game.red && game.blue {

			game.mut.RUnlock()

			return nil, errors.New("Game is full")
		}
		red, blue := game.red, game.blue
		game.mut.RUnlock()
		if !red {

			game.mut.Lock()
			game.red = true
			game.mut.Unlock()

			return &pb.GameIDAndTeam{Id: id.Id, Team: pb.Team_red.Enum()}, nil
		} else if !blue {

			game.mut.Lock()

			game.blue = true
			game.mut.Unlock()

			return &pb.GameIDAndTeam{Id: id.Id, Team: pb.Team_blue.Enum()}, nil
		}
	}
	return nil, errors.New("Game does not exist")
}
func (cs *connect4Server) LeaveGame(ctx context.Context, idAndTeam *pb.GameIDAndTeam) (*pb.Empty, error) {
	if game, exists := cs.games[*idAndTeam.Id]; exists {
		game.mut.Lock()
		if idAndTeam.Team == pb.Team_blue.Enum() {
			game.blue = false
		} else {
			game.red = false
		}
		game.mut.Unlock()

		if !game.blue && !game.red {
			delete(cs.games, *idAndTeam.Id)
		}
	}

	return &pb.Empty{}, nil
}

func (cs *connect4Server) CommunicateState(stream grpc.BidiStreamingServer[pb.Input, pb.State]) error {

	input, err := stream.Recv() // client should send first message with column = -1 in order to place stream in game struct field
	if err == io.EOF {
		return nil
	}
	if err != nil {

		return err
	}
	game, exists := cs.games[*input.GameId]
	if !exists {
		return nil
	}
	if *input.InputTeam == *pb.Team_blue.Enum() {
		game.mut.Lock()
		game.blueStream = stream

		game.mut.Unlock()
		defer func() {
			game.mut.Lock()
			game.blueStream = nil
			game.blue = false
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
		if err == io.EOF {
			return nil
		}
		fmt.Println(input)
		if err != nil {
			return err
		}
		game, exists := cs.games[*input.GameId]
		if !exists {
			return nil
		}

		game.modifyState(*input.Column, *input.InputTeam)
		s := &pb.State{}
		turn := pb.Team(game.turn)
		s.Turn = &turn
		field := pb.Field{Rows: []*pb.Row{}}

		for i := range game.state {
			field.Rows = append(field.Rows, &pb.Row{Values: make([]pb.Team, 8)})
			for j := range game.state {
				field.Rows[i].Values[j] = game.state[i][j]
			}
		}
		s.Field = &field
		err = cs.update(*input.GameId, s)
		if err != nil {
			return err
		}
	}
}
func (cs *connect4Server) NewGame(ctx context.Context, empty *pb.Empty) (*pb.GameIDAndTeam, error) {
	id := rand.Int32()
	_, exists := cs.games[id]
	for exists {
		id = rand.Int32()
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

// CommunicateState(grpc.BidiStreamingServer[Input, State]) error
// NewGame(context.Context, *Empty) (*GameID, error)
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

	if ended {
		g.mut.Lock()
		g.state = field{}
		if inputTeam == *pb.Team_blue.Enum() {
			g.blueWins++
		} else {
			g.redWins++
		}
		g.turn = pb.Team_red
		g.mut.Unlock()
	}

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

type coords struct{ x, y int }
type qNode struct {
	directionStreak int
	streak          int
	coords          coords
}

func scan(state field, team pb.Team) bool {
	visited := make(map[coords]bool)
	queue := make([]qNode, 8)
	for i := range state {
		if state[0][i] == team {
			queue[i] = qNode{-1, 1, coords{0, i}}
		}
	}
	for len(queue) > 0 {
		cur := queue[0]

		visited[cur.coords] = true
		queue = queue[1:]
		fmt.Println(cur)
		if cur.streak >= 4 {
			return true
		}
		toCheck := []coords{coords{cur.coords.x, cur.coords.y + 1}, coords{cur.coords.x + 1, cur.coords.y + 1}, coords{cur.coords.x + 1, cur.coords.y}, coords{cur.coords.x + 1, cur.coords.y - 1}}
		for i, coords := range toCheck {
			if coords.x >= 0 && coords.x < 8 && coords.y >= 0 && coords.y < 8 && state[coords.x][coords.y] == team && !visited[coords] {
				if cur.directionStreak == i {
					queue = append(queue, qNode{cur.directionStreak, cur.streak + 1, coords})
				} else {
					queue = append(queue, qNode{i, 2, coords})
				}
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
	grpcServer.Serve(lis)
}
