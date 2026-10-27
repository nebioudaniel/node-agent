
package main

import "testing"

func TestBytesToGB(t *testing.T) {
    tests := []struct {
        input  uint64
        expect float64
    }{
        {input: 0, expect: 0},
        {input: 1_000_000, expect: 0.001},
        {input: 5_000_000_000, expect: 5},
        {input: 5_000_000, expect: 0.005},
        {input: 6_000_000_000, expect: 6},
    }

    for _, tc := range tests {
        got := BytesToGB(tc.input)

        if got != tc.expect {
            t.Errorf(
                "BytesToGB(%d) = %v; want %v",
                tc.input,
                got,
                tc.expect,
            )
        }
    }
}
