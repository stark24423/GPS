package dtx

import (
	"context"
	"net"
	"testing"
	"time"
)

func TestClearLocationWaitsForDeviceConfirmation(t *testing.T) {
	clientConn, deviceConn := net.Pipe()
	defer deviceConn.Close()

	client := &Client{
		conn:      clientConn,
		nextID:    1,
		replyChan: make(chan message, 1),
	}
	defer client.Close()
	go client.readLoop()

	requestSeen := make(chan message, 1)
	go func() {
		request, err := readMessage(deviceConn)
		if err != nil {
			return
		}
		requestSeen <- request
		device := &Client{conn: deviceConn}
		_ = device.writeMessage(message{
			id:           request.id,
			conversation: 1,
			channelCode:  request.channelCode,
			msgType:      msgOK,
		})
	}()

	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	if err := client.ClearLocation(ctx); err != nil {
		t.Fatalf("ClearLocation() error = %v", err)
	}

	select {
	case request := <-requestSeen:
		if request.flags&flagExpectsReply == 0 {
			t.Fatal("ClearLocation request did not ask for a device confirmation")
		}
	case <-time.After(time.Second):
		t.Fatal("ClearLocation request was not sent")
	}
}
