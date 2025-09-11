package server

import (
	"context"
	"log"
	"testing"

	"github.com/geofpwhite/connect4-grpc/pb"

	"google.golang.org/grpc"
)

func TestConnect4GameFlow(t *testing.T) {
	// Connect to the gRPC server
	conn, err := grpc.Dial("localhost:4040", grpc.WithInsecure())
	if err != nil {
		t.Fatalf("Failed to connect to server: %v", err)
	}
	defer conn.Close()
	client := pb.NewConnect4Client(conn)

	// Start a new game
	newGameResp, err := client.NewGame(context.Background(), &pb.Empty{})
	if err != nil {
		t.Fatalf("Failed to start a new game: %v", err)
	}
	log.Printf("New game started with ID: %d, Team: %v", newGameResp.Id, newGameResp.Team)

	// Make a move

	col := int32(3)
	move := &pb.Input{
		GameId:    newGameResp.Id,
		Column:    &col,
		InputTeam: newGameResp.Team,
	}
	stream, err := client.CommunicateState(context.Background())
	if err != nil {
		t.Fatalf("Failed to communicate state: %v", err)
	}

	if err := stream.Send(move); err != nil {
		t.Fatalf("Failed to send move: %v", err)
	}

	// Receive the updated state
	state, err := stream.Recv()
	if err != nil {
		t.Fatalf("Failed to receive state: %v", err)
	}
	log.Printf("Received state: %+v", state)
	// client2 := pb.NewConnect4Client(conn)

	// Join the game with another client
	joinResp, err := client.JoinGame(context.Background(), &pb.GameID{Id: newGameResp.Id})
	if err != nil {
		t.Fatalf("Failed to join the game: %v", err)
	}

	log.Printf("Joined game with ID: %d, Team: %v", joinResp.Id, joinResp.Team)
}
