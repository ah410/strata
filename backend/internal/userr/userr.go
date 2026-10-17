// Package userr marks errors whose message is safe to show the client.
// Anything else is logged and replaced with a generic message, so OS errors
// carrying host paths never reach the browser.
package userr

// Error is a message written for the user.
type Error string

func (e Error) Error() string { return string(e) }
