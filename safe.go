package sturdyc

import "fmt"

func safeGo(fn func()) {
	go func() {
		defer func() {
			if err := recover(); err != nil {
				//nolint:forbidigo // This should never panic but we want to log it if it does.
				fmt.Println(err)
			}
		}()
		fn()
	}()
}
