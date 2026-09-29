package domain

// ValidationError is a user-facing rule violation. Its message matches the sample's
// wording and is returned by the API as a 422.
type ValidationError struct{ Msg string }

func (e *ValidationError) Error() string { return e.Msg }

func invalid(msg string) error { return &ValidationError{Msg: msg} }
