package server

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log"
	"math/rand/v2"
	"net"
	"sync"

	"github.com/geofpwhite/connect4-grpc/pb"
	"google.golang.org/grpc"
)

type field [8][8]pb.Team

type game struct {
	turn     pb.Team
	state    field
	mut      *sync.RWMutex
	red      bool // true if player1 is connect
	blue     bool //true if player2 is connected
	redWins  int
	blueWins int
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

// CommunicateState(grpc.BidiStreamingServer[Input, State]) error
// NewGame(context.Context, *Empty) (*GameIDAndTeam, error)
// JoinGame(context.Context, *GameID) (*GameIDAndTeam, error)
// LeaveGame(context.Context, *GameIDAndTeam) (*Empty, error)
func (cs *connect4Server) JoinGame(ctx context.Context, id *pb.GameID) (*pb.GameIDAndTeam, error) {
	fmt.Println("joining")
	if game, exists := cs.games[*id.Id]; exists {
		fmt.Println("exists")
		game.mut.RLock()
		fmt.Println("rlocked")
		if game.red && game.blue {
			fmt.Println("full")
			game.mut.RUnlock()
			fmt.Println("runlocked")
			return nil, errors.New("Game is full")
		}
		game.mut.RUnlock()
		if !game.red {
			fmt.Println("new red")
			game.mut.Lock()
			game.red = true
			game.mut.Unlock()
			fmt.Println("new red")
			return &pb.GameIDAndTeam{Id: id.Id, Team: pb.Team_red.Enum()}, nil
		} else if !game.blue {
			fmt.Println("new blue")
			game.mut.Lock()
			fmt.Println("locked")
			game.blue = true
			game.mut.Unlock()
			fmt.Println("new blue")
			return &pb.GameIDAndTeam{Id: id.Id, Team: pb.Team_blue.Enum()}, nil
		}
	}
	return nil, errors.New("Game does not exist")
}
func (cs *connect4Server) LeaveGame(ctx context.Context, idAndTeam *pb.GameIDAndTeam) (*pb.Empty, error) {
	if game, exists := cs.games[*idAndTeam.Id]; exists {
		if idAndTeam.Team == pb.Team_blue.Enum() {
			game.blue = false
		} else {
			game.red = false
		}
	}

	return &pb.Empty{}, nil
}

func (cs *connect4Server) CommunicateState(stream grpc.BidiStreamingServer[pb.Input, pb.State]) error {
	fmt.Println("started communication")
	for {
		input, err := stream.Recv()
		if err == io.EOF {
			return nil
		}
		if err != nil {
			fmt.Println(err)
			return err
		}
		fmt.Println(input)
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
		if err := stream.Send(s); err != nil {
			fmt.Println(err)
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
	if g.state[7][int(column)] != 0 || inputTeam != g.turn {
		fmt.Println("wrong turn", g.turn)
		return
	}
	g.mut.RUnlock()
	g.mut.Lock()
	g.state[7][column] = inputTeam
	g.state = fall(g.state, column)
	g.turn = (g.turn % 2) + 1
	if scan(g.state, inputTeam) {
		g.state = field{}
		if inputTeam == *pb.Team_blue.Enum() {
			g.blueWins++
		} else {
			g.redWins++
		}
		g.turn = pb.Team_red
	}
	g.mut.Unlock()
}

func fall(state field, column int32) field {
	for i := range 8 {
		if state[i][column] != 0 {
			for j := i - 1; j >= 0 && state[j][column] == 0; j-- {
				//set spot j = 1 , j + 1 = 0
				state[j][column], state[j+1][column] = 1, 0
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
	queue := make([]qNode, 8)
	for i := range state {
		if state[i][0] == team {
			queue[i] = qNode{-1, 1, coords{i, 0}}
		}
	}
	for len(queue) > 0 {
		cur := queue[0]
		queue = queue[1:]
		if cur.streak >= 4 {
			return true
		}
		toCheck := []coords{coords{cur.coords.x, cur.coords.y + 1}, coords{cur.coords.x + 1, cur.coords.y + 1}, coords{cur.coords.x + 1, cur.coords.y}, coords{cur.coords.x + 1, cur.coords.y - 1}}
		for i, coords := range toCheck {
			if coords.x >= 0 && coords.x < 8 && coords.y >= 0 && coords.y < 8 && state[coords.x][coords.y] == team {
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

	lis, err := net.Listen("tcp", fmt.Sprintf("localhost:%d", 4040))
	if err != nil {
		log.Fatalf("failed to listen: %v", err)
	}
	var opts []grpc.ServerOption
	grpcServer := grpc.NewServer(opts...)
	pb.RegisterConnect4Server(grpcServer, newServer())
	grpcServer.Serve(lis)
}
