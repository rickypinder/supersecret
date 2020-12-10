package main

import (
	"flag"
	"fmt"
	"net"
	"net/rpc"
	"sync"

	"uk.ac.bris.cs/gameoflife/gol"
)

type Game struct {
	currentlyRunning bool
	world            [][]byte
	currentTurn      int
	p                gol.Params
	quit             bool
	pausechannel     chan bool
	paused           bool
	workers          []*rpc.Client
	shutdownChannel  chan bool
}

// Sends world to worker and updates output slice with the result, if an error is returned by the remote procedure call then
// the problem variable is set to true to indicate the turn needs to be recomputed and the client needs to be removed from
// the logic engine's list of nodes
func (g *Game) getNewState(client *rpc.Client, contextWorld [][]byte, output [][]byte, wg *sync.WaitGroup, problem *bool) {
	err := client.Call("Worker.NextState", contextWorld, &output)
	if err != nil {
		*problem = true
		wg.Done()
		return
	}
	wg.Done()
}

// The actual processing of the world
func (g *Game) start() {
	for g.currentTurn = 0; g.currentTurn < g.p.Turns; g.currentTurn++ {
		if g.paused {
			<-g.pausechannel
			g.paused = false
		}

		var wg sync.WaitGroup

		currentBottom := 0
		num_workers := len(g.workers)
		out := make([][][]byte, num_workers)
		wg.Add(num_workers)

		problem_slice := make([]bool, num_workers)
		var problem bool

		for num, client := range g.workers {
			nextBottom := currentBottom + (g.p.ImageHeight / num_workers)
			if num == num_workers-1 {
				nextBottom = g.p.ImageHeight
			}
			out[num] = make([][]byte, nextBottom-currentBottom)
			for i := range out[num] {
				out[num][i] = make([]byte, g.p.ImageWidth)
			}

			// contexted world includes overlapping rows above and below
			previousRow := gol.Mod(currentBottom-1, g.p.ImageHeight)
			var contextedWorld [][]byte = append(g.world[previousRow:previousRow+1], g.world[currentBottom:nextBottom]...)
			contextedWorld = append(contextedWorld, g.world[gol.Mod(nextBottom, g.p.ImageHeight)])

			go g.getNewState(client, contextedWorld, out[num], &wg, &problem_slice[num])

			currentBottom = nextBottom
		}

		newWorld := make([][]byte, 0)

		wg.Wait() // wait for all the nodes to finish computing
		for i := range problem_slice {
			if problem_slice[i] {
				newClients := make([]*rpc.Client, 0)
				for j := range g.workers {
					if g.workers[j] != g.workers[i] {
						newClients = append(newClients, g.workers[j])
					}
				}
				g.workers = newClients
				problem = true
			}
		}
		if problem { // restart the turn if an error occurs in the remote procedure call to the nodes
			g.currentTurn--
			continue
		}

		for i := range g.workers {
			newWorld = append(newWorld, out[i]...)
		}
		g.world = newWorld
	}
	g.currentlyRunning = false
	return
}

// The function ran by the controller in order to start the processing
func (g *Game) Evolve(a gol.Args, reply *string) (err error) {
	if g.currentlyRunning {
		*reply = "already running"
		return
	}
	g.currentlyRunning = true
	g.p = a.P
	g.world = gol.CalculateWorld(a.Alive, g.p.ImageHeight, g.p.ImageWidth)

	go g.start()
	return
}

const alive = 255
const dead = 0

// returns the current turn to the controller
func (g *Game) CurrentTurn(str string, turn *int) (err error) {
	*turn = g.currentTurn
	return
}

// pauses the start goroutine and sends the controller the current turn
func (g *Game) Pause(str string, turn *int) (err error) {
	fmt.Println("PAUSING")
	g.paused = true
	*turn = g.currentTurn
	return
}

// resumes the start goroutine
func (g *Game) Resume(str string, msg *string) (err error) {
	fmt.Println("RESUMING")
	g.pausechannel <- true
	return
}

// used by the controller to detect if connecting to an already paused process
func (g *Game) IsPaused(str string, paused *bool) (err error) {
	*paused = g.paused
	return
}

// returns the world and current turn to the controller
func (g *Game) GetWorld(str string, wc *gol.Worldcells) (err error) {
	*wc = gol.Worldcells{g.world, g.currentTurn}
	return
}

// returns the current turn and number of alive cells to the controller
// used for AliveCellCount events
func (g *Game) GetTurncells(str string, tc *gol.Turncells) (err error) {
	turn := g.currentTurn
	world := make([][]byte, g.p.ImageHeight)
	for i := range world {
		world[i] = make([]byte, g.p.ImageWidth)
	}

	copy(world, g.world)
	*tc = gol.Turncells{turn, len(gol.CalculateAliveCells(world))}
	return
}

// used by the controller to detect when processing has finisehd
func (g *Game) IsFinished(str string, done *bool) (err error) {
	*done = !g.currentlyRunning
	return
}

// called by the nodes to give the
func (g *Game) Subscribe(address string, reply *string) (err error) {
	fmt.Println("Worker Request from ", address)
	client, err := rpc.Dial("tcp", address)
	if err != nil {
		fmt.Println("Error subscribing ", address)
		fmt.Println(err)
		return
	}
	g.workers = append(g.workers, client)
	return
}

// closes each worker before shutting down the logic engine
func (g *Game) Shutdown(msg string, reply *string) (err error) {
	for _, v := range g.workers {
		v.Call("Worker.Shutdown", "", nil)
	}
	g.shutdownChannel <- true
	return
}

// registers the functions to rpc, creates the listener and accepts incomming connections from both the controller and nodes
func AcceptConnections(pAddr string, g *Game) {
	rpc.Register(g)
	listener, err := net.Listen("tcp", ":"+pAddr)
	fmt.Println("created listener")
	if err != nil {
		panic(err)
	}
	rpc.Accept(listener)
	listener.Close()
}

func main() {
	pAddr := flag.String("port", "8030", "port to listen on")
	pauseChannel := make(chan bool)
	shutdownChannel := make(chan bool)
	flag.Parse()

	// create an initial game struct
	game := &Game{false, nil, 0, gol.Params{}, false, pauseChannel, false, []*rpc.Client{}, shutdownChannel}

	go AcceptConnections(*pAddr, game)
	<-shutdownChannel
}
