package my_module

import (
	"dagger.io/dagger/core"
)

type MyModule struct {
	AAgent ApproverAgent
}

type ApproverAgent struct {
	GithubAppKey *core.Secret
	SSH          *core.Socket
}

func New() *MyModule {
	return &MyModule{
		AAgent: ApproverAgent{},
	}
}
