// +build go1.8

package test

import (
	"io"
	"os"
	"regexp"
	"testing"
)

/* This file contains copies of basic functionality from the testing package */

// testDeps is a copy of testing.testDeps
type testDeps interface {
	MatchString(pat, str string) (bool, error)
	StartCPUProfile(io.Writer) error
	StopCPUProfile()
	WriteHeapProfile(io.Writer) error
	WriteProfileTo(string, io.Writer, int) error
}

type deps struct{}

var _ testDeps = &deps{}

func (d *deps) MatchString(pat, str string) (bool, error)         { return regexp.MatchString(pat, str) }
func (d *deps) StartCPUProfile(_ io.Writer) error                 { return nil }
func (d *deps) StopCPUProfile()                                   {}
func (d *deps) WriteHeapProfile(_ io.Writer) error                { return nil }
func (d *deps) WriteProfileTo(_ string, _ io.Writer, _ int) error { return nil }

func mainStart(clients *Clients, suites []string, rw bool) {
	m := testing.MainStart(&deps{}, gatherTests(clients, suites, rw), nil, nil)
	os.Exit(m.Run())
}