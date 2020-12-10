package gol

import (
	"fmt"
	"net/rpc"
	"os"
	"time"
	"uk.ac.bris.cs/gameoflife/util"
)

type distributorChannels struct {
	events     chan<- Event
	ioCommand  chan<- ioCommand
	ioIdle     <-chan bool
	input      <-chan uint8
	output     chan<- uint8
	filepath   chan<- string
	keyPresses <-chan rune
}

// Struct used for AliveCell events
type Turncells struct {
	Turn      int
	Num_cells int
}

// Struct used for receiving the world from the logic engine
type Worldcells struct {
	World [][]byte
	Turn  int
}

// Struct used for the initial sending of data to the logic engine
type Args struct {
	P     Params
	Alive []util.Cell
}


// The controller struct
type Controller struct {
	p      Params
	c      distributorChannels
	paused bool
	client *rpc.Client
}

// Constructor for controller
func createController(p Params, c distributorChannels) *Controller {
	return &Controller{p, c, false, nil}
}

// This function polls the logic engine every 2 seconds for the turn number and the number of alive cells
// and then sends this in an AliveCellsCount event down the events channel
func (con *Controller) aliveCellsEvents(events chan<- Event, done <-chan bool) {
	ticker := time.NewTicker(2 * time.Second)

	for {
		select {
		case <-done:
			return
		case <-ticker.C:
			tc := Turncells{0, 0}
			con.client.Call("Game.GetTurncells", "", &tc)
			events <- AliveCellsCount{tc.Turn, tc.Num_cells}
		}
	}
}

// Updates the live sdl by polling the logic engine every 100ms to get the current
// state of the world (and the current turn). It then sends cell flipped events
// (which have been modified from the default behaviour to use the SetPixel instead
// of FlipPixel function) down the event channel for each alive cell before sending
// a TurnComplete event which causes the sdl to clear the display and then render the
// current pixels array
func (con *Controller) updateDisplay(done chan bool) {
	ticker := time.NewTicker(100 * time.Millisecond)
	world := make([][]byte, con.p.ImageHeight)
	for i := range world {
		world[i] = make([]byte, con.p.ImageWidth)
	}
	con.c.events <- TurnComplete{0}

	for {
		select {
		case <-done:
			return
		case <-ticker.C:
			wc := Worldcells{world, 0}
			err := con.client.Call("Game.GetWorld", "", &wc)
			if err != nil {
				fmt.Println("Error with updating display:", err)
				return
			}
			for y := 0; y < con.p.ImageHeight; y++ {
				for x := 0; x < con.p.ImageWidth; x++ {
					if world[y][x] == 255 {
						con.c.events <- CellFlipped{wc.Turn, util.Cell{X: x, Y: y}}
					}
				}
			}
			con.c.events <- TurnComplete{wc.Turn}
		}
	}
}

// Polls the logic engine every ms to find out if the currently running game has finished
// if so then a value if sent down the done channel to signify this to the other goroutines
func (con *Controller) waitForFinish(done chan<- bool) {
	ticker := time.NewTicker(1 * time.Millisecond)

	for {
		select {
		case <-ticker.C:
			var finished bool
			con.client.Call("Game.IsFinished", "", &finished)
			if finished {
				done <- true
				return
			}
		}
	}
}

// Uses the distributor channels to return a slice containing the world
func (con *Controller) readInWorld() [][]byte {
	newWorld := make([][]byte, con.p.ImageHeight)
	for i := range newWorld {
		newWorld[i] = make([]byte, con.p.ImageWidth)
	}
	//Tell io routine to start reding the pgm file and putting it on input
	con.c.ioCommand <- ioInput
	//create and start populating the rows
	for y := 0; y < con.p.ImageHeight; y++ {
		for x := 0; x < con.p.ImageWidth; x++ {
			val := <-con.c.input
			newWorld[y][x] = val
		}
	}

	return newWorld
}

// handles the keypresses from sdl and reacts accordingly
func (con *Controller) handleKeypresses(done chan bool, display_update_done chan bool, alive_cells_done chan bool) {
	var finished bool
	for !finished {
		select {
		case <-done:
			finished = true
			display_update_done <- true
			alive_cells_done <- true
		case key := <-con.c.keyPresses:
			switch key {
			case 's': // Generate PGM file with current state of the board
				con.writeOutWorld()
			case 'q': // Close controller
				finished = true
				display_update_done <- true
				alive_cells_done <- true
			case 'p': // Pause logic engine
				if !con.paused {
					var turn int
					con.client.Call("Game.Pause", "", &turn)
					fmt.Println("Pausing on turn ", turn)
					con.paused = true
				} else {
					con.client.Go("Game.Resume", "", nil, nil)
					fmt.Println("Resuming")
					con.paused = false
				}
			case 'k': // All components of the system are shut down cleanly and output pgm image of latest state
				display_update_done <- true
				alive_cells_done <- true

				con.writeOutWorld()
				
				con.client.Call("Game.Shutdown", "", nil)
				os.Exit(0)
			}
		}
	}
}

// The main controller function that reads the image, connects to the logic engine, handles the keypresses,
// and outputs the final image
func (con *Controller) run() {
	newWorld := con.readInWorld()

	// Connect to logic engine
	address := os.Getenv("SERVER")
	var err error
	con.client, err = rpc.Dial("tcp", address)
	defer con.client.Close()
	fmt.Println("connected to logic engine")
	if err != nil {
		panic(err)
	}

	// Checks if connecting to an already paused instance
	con.client.Call("Game.IsPaused", "", &con.paused)

	done := make(chan bool)
	display_update_done := make(chan bool)
	alive_cells_done := make(chan bool)

	var msg string
	con.client.Call("Game.Evolve", Args{con.p, CalculateAliveCells(newWorld)}, &msg)
	if msg == "already running" {
		fmt.Println("Connecting to already running gol instance")
	}

	go con.aliveCellsEvents(con.c.events, alive_cells_done)
	go con.waitForFinish(done)
	go con.updateDisplay(display_update_done)

	con.handleKeypresses(done, display_update_done, alive_cells_done)
	fmt.Println("finishing")

	wc := Worldcells{newWorld, 0}
	con.client.Call("Game.GetWorld", "", &wc)
	con.c.events <- FinalTurnComplete{con.p.Turns, CalculateAliveCells(newWorld)}

	fmt.Println("writing image")
	//output board as pgm image
	con.writeImage(newWorld, wc.Turn)
	con.c.events <- ImageOutputComplete{con.p.Turns, fmt.Sprintf("%dx%d", con.p.ImageWidth, con.p.ImageHeight)}

	fmt.Println("terminating")
	con.terminateGracefully()
}

func (con *Controller) terminateGracefully() {
	// Make sure that the IO has finished any output before exiting
	con.c.ioCommand <- ioCheckIdle
	<-con.c.ioIdle

	// Close the channel to stop the SDL goroutine gracefully. Removing may cause deadlock.
	con.c.events <- StateChange{con.p.Turns, Quitting}
	close(con.c.events)
}

// Turns the world into a list of all alive cells
func CalculateAliveCells(world [][]byte) []util.Cell {
	aliveCells := []util.Cell{}

	height := len(world)
	width := len(world[0])

	for y := 0; y < height; y++ {
		for x := 0; x < width; x++ {
			if world[y][x] == 255 {
				aliveCells = append(aliveCells, util.Cell{X: x, Y: y})
			}
		}
	}

	return aliveCells
}

// Turns alive cells into world matrix
func CalculateWorld(alive_cells []util.Cell, height, width int) [][]byte {
	world := make([][]byte, height)
	for i := range world {
		world[i] = make([]byte, width)
	}

	for _, cell := range alive_cells {
		world[cell.Y][cell.X] = 255
	}

	return world
}

// taking in a world and the current turn this method will create the output image
func (con *Controller) writeImage(newWorld [][]byte, turn int) {
	height := len(newWorld)
	width := len(newWorld[0])

	con.c.ioCommand <- ioOutput
	con.c.filepath <- fmt.Sprintf("%dx%dx%d", width, height, turn)
	//create and start populating the rows
	for y := 0; y < height; y++ {
		for x := 0; x < width; x++ {
			con.c.output <- newWorld[y][x]
		}
	}
}

// fetches the world from the logic engine and writes the image out
func (con *Controller) writeOutWorld() {
	world := make([][]byte, con.p.ImageHeight)
	for i := range world {
		world[i] = make([]byte, con.p.ImageWidth)
	}
	wc := Worldcells{world, 0}
	con.client.Call("Game.GetWorld", "", &wc)
	con.writeImage(world, wc.Turn)
}

// Optimised mod function
func Mod(x, m int) int {
	if x < 0 {
		return x + m
	} else if x >= m {
		return x - m
	} else {
		return x
	}
}
