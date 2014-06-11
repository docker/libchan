package data

import (
	"testing"
)

func TestEmptyMessage(t *testing.T) {
	m := Empty()
	if m.String() != Encode(nil) {
		t.Fatalf("%v != %v", m.String(), Encode(nil))
	}
}

func TestSetMessage(t *testing.T) {
	m := Empty().Set("foo", "bar")
	output := m.String()
	expectedOutput := "000;3:foo,6:3:bar,,"
	if output != expectedOutput {
		t.Fatalf("'%v' != '%v'", output, expectedOutput)
	}
	decodedOutput, err := Decode(output)
	if err != nil {
		t.Fatal(err)
	}
	if len(decodedOutput) != 1 {
		t.Fatalf("wrong output data: %#v\n", decodedOutput)
	}
	if len(decodedOutput["foo"]) != 1 {
		t.Fatalf("Key foo contains %d values, expected 1", len(decodedOutput["foo"]))
	}
	if decodedOutput["foo"][0] != "bar" {
		t.Fatalf("Key foo is %v, expected bar", decodedOutput["foo"][0])
	}
}

func TestSetMessageTwice(t *testing.T) {
	m := Empty().Set("foo", "bar").Set("ga", "bu")
	output := m.String()
	// Check two outputs because map is unordered
	expectedOutput1 := "000;3:foo,6:3:bar,,2:ga,5:2:bu,,"
	expectedOutput2 := "000;2:ga,5:2:bu,,3:foo,6:3:bar,,"
	if output != expectedOutput1 && output != expectedOutput2 {
		t.Fatalf("'%v' != '%v' and '%v' != '%v'", output, expectedOutput1, output, expectedOutput2)
	}
	decodedOutput, err := Decode(output)
	if err != nil {
		t.Fatal(err)
	}
	if len(decodedOutput) != 2 {
		t.Fatalf("wrong output data: %#v\n", decodedOutput)
	}
	if len(decodedOutput["foo"]) != 1 {
		t.Fatalf("Key foo contains %d values, expected 1", len(decodedOutput["foo"]))
	}
	if len(decodedOutput["ga"]) != 1 {
		t.Fatalf("Key ga contains %d values, expected 1", len(decodedOutput["ga"]))
	}
	if decodedOutput["foo"][0] != "bar" {
		t.Fatalf("Key foo is %v, expected bar", decodedOutput["foo"][0])
	}
	if decodedOutput["ga"][0] != "bu" {
		t.Fatalf("Key ga is %v, expected bu", decodedOutput["ga"][0])
	}
}

func TestSetDelMessage(t *testing.T) {
	m := Empty().Set("foo", "bar").Del("foo")
	output := m.String()
	expectedOutput := Encode(nil)
	if output != expectedOutput {
		t.Fatalf("'%v' != '%v'", output, expectedOutput)
	}
}
