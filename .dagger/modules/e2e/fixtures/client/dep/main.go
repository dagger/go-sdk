package main

type ClientDep struct{}

func (m *ClientDep) Greet(name string) string {
	return "hello " + name
}
