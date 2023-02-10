package watcher

import "testing"

func TestPattern(t *testing.T) {
	testMatch := func(input, pattern string, expectMatch bool) {
		if match([]byte(pattern), []byte(input)) != expectMatch {
			if expectMatch {
				t.Fatalf("Expected pattern %v to match %v, but it did not", pattern, input)
			} else {
				t.Fatalf("Expected pattern %v to not match %v, but it did", pattern, input)
			}
		}
	}

	testMatch("hello", "hello", true)
	testMatch("hello", "hello", true)
	testMatch("hello", "byeye", false)
	testMatch("hello", "h*", true)
	testMatch("hello", "he*", true)
	testMatch("hello", "*lo", true)
	testMatch("hello", "he*o", true)
	testMatch("heol", "he*o", false)
	testMatch("main.go", "*.go", true)
	testMatch("main.go", "*n.go", true)
	testMatch("main.go", "*s.go", false)
	testMatch("main.go", "*go2", false)
	testMatch("pkg/watcher/watcher_test.go", "*.go", false)
	testMatch("pkg/watcher/watcher_test.go", "*/*/*.go", true)
	testMatch("pkg/watcher/watcher_test.go", "*/*.go", false)
	testMatch("pkg/watcher/watcher_test.go", "**/**.go", true)
	testMatch("pkg/watcher/watcher_test.go", "**.go", true)
	testMatch("pkg/watcher/watcher_test.go", "**.go", true)
	testMatch("pkg/watcher/watcher_test.go", "**", true)
	testMatch(".abc", "**", true)
}
