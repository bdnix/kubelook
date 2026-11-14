package execs

// Open exposes the channel opener to the black-box tests, with a plain Sink instead of a connection.
var Open = open

// Candidates exposes the shell fallback to tests.
var StartShell = startShell
