package httpapi

// Test-only exports of unexported helpers, so black-box tests can drive them.
var (
	WriteError = writeError
	WriteJSON  = writeJSON
	DecodeJSON = decodeJSON
)
