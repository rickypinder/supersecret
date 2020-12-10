package main

import (
	"flag"
	"fmt"
	"net"
	"net/rpc"
	"sync"

	"uk.ac.bris.cs/gameoflife/gol"
)

const alive = 255
const dead = 0

type Worker struct {
	shutdownChannel chan bool
	threadNumber int
	strips       chan stripInfo
}

// called by the logic engine to shutdown the node
func (w *Worker) Shutdown(msg string, reply *string) (err error) {
	w.shutdownChannel <- true
	return
}

type worldInfo struct {
	width  int
	height int
}

//Assume that it is always the full width of the world
//Information about the strip of the world for a worker to process
type stripInfo struct {
	world  [][]uint8  //The world to act upon
	out    *[][]uint8 //The world to write to
	top    int        //The top row to start on
	bottom int        //The first non-altered row number
	w      worldInfo  //There is no Params here, so it needs its own lil' struct
	wait   *sync.WaitGroup
}

func workerFunction(strips <-chan stripInfo) {
	for {
		info := <-strips
		calculateNextStateOfStrip(&info.world, info.out, info.top, info.bottom, info.w)
		info.wait.Done()
	}
}



// the main function of the worker called by the logic engine to process the world
func (w *Worker) NextState(world [][]byte, out *[][]byte) (err error) {
	boardHeight := len(world)
	boardWidth := len(world[0])
	*out = calculateNextState(world, boardHeight, boardWidth, w.strips, w.threadNumber)[1 : len(world)-1]
	return
}

// probably replace
func (w *Worker) spawnWorkerThreads() () {
	for i := 0; i < w.threadNumber; i++ {
		go workerFunction(w.strips)
	}
	return
}

// //###################################################################//
//### The following functions calculate the number of alive cells ###//
//### neighbouring the given cell. This is much faster than using ###//
//### a single mod function for all (2.22s -> 1.51s). While they  ###//
//### could be generalised using higher order functions, this     ###//
//### counteracts all performance benefits (2.12s), so we elected ###//
//### to keep with this method.                                   ###//
//###################################################################//

func calculateNeighbours(x, y int, world [][]byte, height, width int) int {
	neighbours := 0
	for i := -1; i <= 1; i++ {
		for j := -1; j <= 1; j++ {
			if i != 0 || j != 0 {
				if world[y+i][x+j] == alive {
					neighbours++
				}
			}
		}
	}
	return neighbours
}

func calculateNeighboursClampX(x, y int, world [][]byte, height, width int) int {
	neighbours := 0
	for i := -1; i <= 1; i++ {
		for j := -1; j <= 1; j++ {
			if i != 0 || j != 0 {
				if world[y+i][gol.Mod(x+j, width)] == alive {
					neighbours++
				}
			}
		}
	}
	return neighbours
}

func calculateNeighboursClampY(x, y int, world [][]byte, height, width int) int {
	neighbours := 0
	for i := -1; i <= 1; i++ {
		for j := -1; j <= 1; j++ {
			if i != 0 || j != 0 {
				if world[gol.Mod(y+i, height)][x+j] == alive {
					neighbours++
				}
			}
		}
	}
	return neighbours
}

func calculateNeighboursClamp(x, y int, world [][]byte, height, width int) int {
	neighbours := 0
	for i := -1; i <= 1; i++ {
		for j := -1; j <= 1; j++ {
			if i != 0 || j != 0 {
				if world[gol.Mod(y+i, height)][gol.Mod(x+j, width)] == alive {
					neighbours++
				}
			}
		}
	}
	return neighbours
}

func setAliveDead(world [][]byte, newWorld [][]byte, x, y, neighbours int) {
	if world[y][x] == alive {
		if neighbours == 2 || neighbours == 3 {
			newWorld[y][x] = alive
		} else {
			newWorld[y][x] = dead
		}
	} else {
		if neighbours == 3 {
			newWorld[y][x] = alive
		} else {
			newWorld[y][x] = dead
		}
	}
}

func calculateNextStateOfStrip(world, out *[][]byte, top, bottom int, w worldInfo) {

	var sideFunc func(int, int, [][]byte, int, int) int
	var midFunc func(int, int, [][]byte, int, int) int

	for y := top; y < bottom; y++ {
		if y == 0 || y == w.height-1 {
			sideFunc = calculateNeighboursClamp
			midFunc = calculateNeighboursClampY
		} else {
			sideFunc = calculateNeighboursClampX
			midFunc = calculateNeighbours
		}

		neighbours := sideFunc(0, y, *world, w.height, w.width)
		setAliveDead(*world, *out, 0, y, neighbours)
		for x := 1; x < w.width-1; x++ {
			neighbours = midFunc(x, y, *world, w.height, w.width)
			setAliveDead(*world, *out, x, y, neighbours)
		}
		neighbours = sideFunc(w.width-1, y, *world, w.height, w.width)
		setAliveDead(*world, *out, w.width-1, y, neighbours)
	}
}

func calculateNextState(world [][]byte, height, width int, stripChannel chan<- stripInfo, threadNumber int) [][]byte {
	newWorld := make([][]byte, height)
	for i := 0; i < height; i++ {
		newWorld[i] = make([]byte, width)
	}

	threadsToMake := threadNumber
	for threadsToMake > height {
		threadsToMake -= 1
	}
	w := worldInfo{width: width, height: height}
	var wg sync.WaitGroup
	wg.Add(threadsToMake)
	currentBottom := 0

	//May not cleanly divide
	for i := 0; i < threadsToMake-1; i++ {
		nextBottom := currentBottom + (height / threadsToMake) //compiler can optimise this line
		stripChannel <- stripInfo{world: world, out: &newWorld, top: currentBottom, bottom: nextBottom, w: w, wait: &wg}
		currentBottom = nextBottom
	}
	stripChannel <- stripInfo{world: world, out: &newWorld, top: currentBottom, bottom: height, w: w, wait: &wg}

	wg.Wait()

	return newWorld
}

// connects to the logic engine and subscribes with its own address, this allows the logic engine to access the NextState function on this node
func connectToEngine(client *rpc.Client, pAddr string, engineAddr string) {
	var msg string
	err := client.Call("Game.Subscribe", pAddr, &msg)
	fmt.Println("connected to engine")
	if err != nil {
		panic(err)
	}
	fmt.Println(msg)
}

func main() {
	pAddr := flag.String("ip", "127.0.0.1:8050", "IP and port to listen on")
	engineAddr := flag.String("engine", "127.0.0.1:8030", "Address of the logic engine")
	threads := flag.Int("threads", 4, "number of threads to use for computation")
	flag.Parse()

	shutdownChannel := make(chan bool)
	strips := make(chan stripInfo)

	worker := Worker{shutdownChannel, *threads, strips}
	worker.spawnWorkerThreads()

	rpc.Register(&worker)
	listener, err := net.Listen("tcp", *pAddr)
	if err != nil {
		panic(err)
	}
	client, err := rpc.Dial("tcp", *engineAddr)
	defer client.Close()
	if err != nil {
		panic(err)
	}
	go connectToEngine(client, *pAddr, *engineAddr)
	go rpc.Accept(listener)
	<- shutdownChannel
}
