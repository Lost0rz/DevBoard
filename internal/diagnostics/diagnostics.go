// Package diagnostics defines the narrow observer seam shared by the Hub
// runtime and the Operator Console. Implementations decide where bounded,
// redacted events are retained; producers can only submit catalogued events.
package diagnostics

// Recorder accepts an allow-listed product event without request data,
// credentials, paths, or free-form error text.
type Recorder interface {
	Record(level, component, event string)
}
