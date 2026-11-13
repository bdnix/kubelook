// This file makes web/ a separate Go module so that "go ./..." and the Go tools
// skip Go files that npm packages ship inside web/node_modules. It has no code.
module example.invalid/kubelook-web-ignore

go 1.27.4
