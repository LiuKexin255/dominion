package main

type App struct{}

func (a *App) Greet(name string) string {
	return "Hello, " + name
}

func main() {
}
