package validation

// Error carries a client-safe validation message for one package domain.
type Error[Domain any] struct {
	Message string
}

func (e *Error[Domain]) Error() string { return e.Message }
