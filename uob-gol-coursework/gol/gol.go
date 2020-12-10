package gol

import "fmt"

// Params provides the details of how to run the Game of Life and which image to load.
type Params struct {
	Turns       int
	Threads     int
	ImageWidth  int
	ImageHeight int
}

// Run starts the processing of Game of Life. It should initialise channels and goroutines.
func Run(p Params, events chan<- Event, keyPresses <-chan rune) {

	ioCommand := make(chan ioCommand)
	ioIdle := make(chan bool)
	//Don't read and write one by one
	inputData := make(chan uint8, p.ImageWidth*p.ImageHeight)
	outputData := make(chan byte, p.ImageWidth * p.ImageHeight)
	filenameChannel := make(chan string, 5)

	theJankyFilename := fmt.Sprintf("%dx%d", p.ImageWidth, p.ImageHeight)
	filenameChannel <- theJankyFilename

	distributorChannels := distributorChannels{
		events,
		ioCommand,
		ioIdle,
		inputData,
		outputData,
		filenameChannel,
		keyPresses,
	}

	ioChannels := ioChannels{
		command:  ioCommand,
		idle:     ioIdle,
		filename: filenameChannel,
		output:   outputData,
		input:    inputData,
	}

	controller := createController(p, distributorChannels)
	go controller.run()
	go startIo(p, ioChannels)
}
