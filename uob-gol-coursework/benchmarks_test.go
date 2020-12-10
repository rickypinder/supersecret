package main

import (
	"testing"
	"uk.ac.bris.cs/gameoflife/gol"
	"fmt"
)

/*func Benchmark16( b *testing.B) {
	benchmarkGol(b, makeParam(1000, 16))
}

func Benchmark64( b *testing.B) {
	benchmarkGol(b, makeParam(1000, 64))
}

func Benchmark128( b *testing.B) {
	benchmarkGol(b, makeParam(1000, 128))
}

func Benchmark256( b *testing.B) {
	benchmarkGol(b, makeParam(1000, 256))
}

func Benchmark512( b *testing.B) {
	benchmarkGol(b, makeParam(1000, 512))
}*/
func Benchmark5120(b *testing.B) {
	benchmarkGol(b, makeParam(100, 5120))
}

func makeParam(turns, size int) gol.Params {
	return gol.Params{Turns: turns, ImageWidth: size, ImageHeight: size}
}


func benchmarkGol(b *testing.B, p gol.Params) {
	fmt.Println(b.N)
	for n := 0; n < b.N; n++ {
		events := make(chan gol.Event)
		fmt.Println("AAAAAA")
		gol.Run(p, events, nil)
		for event := range events {
			switch event.(type) {
			case gol.FinalTurnComplete:
				//it finishes
				return
			}
		}
	}
}
