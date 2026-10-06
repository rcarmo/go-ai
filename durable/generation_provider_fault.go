package durable

// Provider callbacks that throw are task faults; terminal provider error messages
// remain ordinary model failures/retries. Storage/admission failures never use it.
type generationProviderFault struct{ cause error }

func (e *generationProviderFault) Error() string { return e.cause.Error() }
func (e *generationProviderFault) Unwrap() error { return e.cause }
