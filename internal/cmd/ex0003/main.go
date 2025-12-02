package main

import "context"

// cancel を複数回呼んでも良い
func main() {
	_, cancel := context.WithCancel(context.Background())
	cancel()
	cancel()
}
