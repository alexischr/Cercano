//go:build !darwin && !linux

package tuirestart

import "errors"

func Supported() bool                  { return false }
func Target() (string, error)          { return "", errors.New("restart this TUI manually") }
func CheckTarget(string) error         { return errors.New("restart this TUI manually") }
func Replace(string, State) error      { return errors.New("restart this TUI manually") }
func ReadInherited(int) (State, error) { return State{}, ErrState }
