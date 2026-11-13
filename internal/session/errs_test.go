package session_test

import "errors"

func errorsAs(err error, target any) bool { return errors.As(err, target) }
func errorsIs(err, target error) bool     { return errors.Is(err, target) }
