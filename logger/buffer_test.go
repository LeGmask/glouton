package logger

import (
	"bytes"
	"fmt"
	"math/rand"
	"regexp"
	"strconv"
	"strings"
	"testing"
	"time"
)

func Test_bufferSize(t *testing.T) {
	re := regexp.MustCompile(`this is line #(\d+)`)
	b := &buffer{}

	const (
		maxLine  = 100000
		headSize = 10000
		tailSize = 20000
	)

	b.SetCapacity(headSize, tailSize)

	var tailAlreadyPresent bool

	for lineNumber := 0; lineNumber < maxLine; lineNumber++ {
		line := fmt.Sprintf("this is line #%d. This random is to reduce compression %d%d%d\n", lineNumber, rand.Int(), rand.Int(), rand.Int()) //nolint: gosec

		n, err := b.write(time.Now(), []byte(line))
		if err != nil {
			t.Fatal(err)
		}

		if n != len([]byte(line)) {
			t.Fatalf("Write() = %d, want %d", n, len([]byte(line)))
		}

		if b.state == stateWritingTail && !tailAlreadyPresent {
			tailAlreadyPresent = true

			content := b.Content()
			if bytes.Contains(content, []byte("[...]")) {
				t.Errorf("content already had elipis marked but shouldn't")
			}
		}
	}

	for readNumber := 1; readNumber < 3; readNumber++ {
		t.Run(fmt.Sprintf("readnumber=%d", readNumber), func(t *testing.T) {
			var (
				hadError         bool
				hadElipsisMarker bool
			)

			seenNumber := make(map[int]bool)

			content := b.Content()
			for i, line := range strings.Split(string(content), "\n") {
				if line == "" {
					continue
				}

				if line == "[...]" {
					hadElipsisMarker = true

					continue
				}

				match := re.FindStringSubmatch(line)
				if match == nil {
					t.Errorf("line %#v (#%d) don't match RE", line, i)

					hadError = true

					continue
				}

				n, err := strconv.ParseInt(match[1], 10, 0)
				if err != nil {
					t.Error(err)

					hadError = true

					continue
				}

				seenNumber[int(n)] = true
			}

			for _, n := range []int{0, 1, 2} {
				if !seenNumber[n] {
					t.Errorf("line #%d should be present, but isn't", n)
					hadError = true
				}
			}

			var (
				headEndAt   int
				tailStartAt int
			)

			for n := 0; n < maxLine; n++ {
				if !seenNumber[n] && headEndAt == 0 {
					headEndAt = n
				}

				if headEndAt != 0 && seenNumber[n] {
					tailStartAt = n

					break
				}
			}

			if headEndAt == 0 {
				t.Errorf("no absent value. Buffer is too large / test don't write enough")
				hadError = true
			}

			if tailStartAt == 0 {
				t.Errorf("no starting tail. This isn't expected. firstAbsent=%d", headEndAt)
				hadError = true
			}

			for n := headEndAt; n < tailStartAt; n++ {
				if seenNumber[n] {
					t.Errorf("line %d is present, but shouldn't (head end=%d, tail start=%d)", n, headEndAt, tailStartAt)
					hadError = true

					break
				}
			}

			for n := tailStartAt; n < maxLine; n++ {
				if !seenNumber[n] {
					t.Errorf("line %d isn't present, but should (head end=%d, tail start=%d)", n, headEndAt, tailStartAt)
					hadError = true

					break
				}
			}

			if !hadElipsisMarker {
				t.Error("elipis marked \"[...]\" not found")

				hadError = true
			}

			// +6 is for the elipis marked and its newline
			if len(content) > headSize+tailSize+6 {
				t.Errorf("len(content) = %d, want < %d", len(content), headSize+tailSize+6)

				hadError = true
			}

			if len(content) < headSize+tailSize/2 {
				t.Errorf("len(content) = %d, want > %d", len(content), headSize+tailSize/2)

				hadError = true
			}

			if b.head.Len() > b.headMaxSize {
				t.Errorf("head size = %d, want < %d", b.head.Len(), b.headMaxSize)

				hadError = true
			}

			for i := range b.tails {
				if b.tails[i].Len() > b.tailMaxSize {
					t.Errorf("tails[%d] size = %d, want < %d", i, b.tails[i].Len(), b.tailMaxSize)

					hadError = true
				}
			}

			if hadError {
				if len(content) < 150 {
					t.Logf("content is %#v", string(content))
				} else {
					t.Logf("content[:150] is %#v", string(content[:150]))
					t.Logf("content[-150:] is %#v", string(content[len(content)-150:]))
				}
			}
		})
	}
}

func Test_bufferOrder(t *testing.T) {
	re := regexp.MustCompile(`this is line #(\d+)`)
	b := &buffer{}

	const (
		headSize = 10000
		tailSize = 20000
	)

	b.SetCapacity(headSize, tailSize)

	writeSize := []int{10, 100, 150, 200, 250, 1000, 10000, 100000}

	var (
		lineNumber             int
		elispseAfterCountLines int
	)

	rnd := rand.New(rand.NewSource(42)) //nolint: gosec

	for _, maxLine := range writeSize {
		t.Run(fmt.Sprintf("maxLine=%d", maxLine), func(t *testing.T) {
			for ; lineNumber < maxLine; lineNumber++ {
				line := fmt.Sprintf("this is line #%d. This random is to reduce compression %d%d%d\n", lineNumber, rnd.Int(), rnd.Int(), rnd.Int())

				n, err := b.write(time.Now(), []byte(line))
				if err != nil {
					t.Fatal(err)
				}

				if n != len([]byte(line)) {
					t.Fatalf("Write() = %d, want %d", n, len([]byte(line)))
				}
			}

			t.Logf("tailIndex=%d, droppedFirstTail=%v", b.tailIndex, b.droppedFirstTail)

			seenNumber := make(map[int]int)
			content := b.Content()

			var (
				hadError         bool
				hadElipsisMarker bool
				elipsisMarkerAt  int
				lastNumber       int64
			)

			for i, line := range strings.Split(string(content), "\n") {
				if line == "" {
					continue
				}

				if line == "[...]" {
					hadElipsisMarker = true
					elipsisMarkerAt = i
					t.Logf("Elipsis at line #%d, lastNumber=%d", i, lastNumber)

					continue
				}

				match := re.FindStringSubmatch(line)
				if match == nil {
					t.Errorf("line %#v (#%d) don't match RE", line, i)

					hadError = true

					continue
				}

				n, err := strconv.ParseInt(match[1], 10, 0)
				if err != nil {
					t.Error(err)

					hadError = true

					continue
				}

				if n < lastNumber {
					t.Errorf("line #%d has number %d but previous number was bigger (%d)", i, n, lastNumber)
				}

				if n != lastNumber+1 && !hadElipsisMarker && i != 0 {
					t.Errorf("line #%d has number %d but expected %d", i, n, lastNumber+1)
				}

				if n != lastNumber+1 && hadElipsisMarker && elipsisMarkerAt != i-1 {
					t.Errorf("line #%d has number %d but expected %d", i, n, lastNumber+1)
				}

				if hadElipsisMarker && i == elipsisMarkerAt+1 {
					t.Logf("line #%d is number %d (just after elipsis)", i, n)
				}

				lastNumber = n

				if _, ok := seenNumber[int(n)]; ok {
					t.Errorf("line #%d has number %d which was already seen at line %d", i, n, seenNumber[int(n)])
				}

				seenNumber[int(n)] = i
			}

			if elispseAfterCountLines == 0 && hadElipsisMarker {
				elispseAfterCountLines = maxLine
			}

			t.Logf("hadElipsisMarker=%v, elispseAfterCountLines=%d", hadElipsisMarker, elispseAfterCountLines)

			if hadError {
				if len(content) < 150 {
					t.Logf("content is %#v", string(content))
				} else {
					t.Logf("content[:150] is %#v", string(content[:150]))
					t.Logf("content[-150:] is %#v", string(content[len(content)-150:]))
				}
			}
		})
	}
}

func Benchmark_buffer(b *testing.B) {
	buff := &buffer{}

	const (
		maxLine  = 100000
		headSize = 5000
		tailSize = 5000
	)

	buff.SetCapacity(headSize, tailSize)

	for n := 0; n < b.N; n++ {
		line := fmt.Sprintf("this is line #%d. This random is to reduce compression %d%d%d\n", n, rand.Int(), rand.Int(), rand.Int()) //nolint: gosec

		n, err := buff.write(time.Now(), []byte(line))
		if err != nil {
			b.Fatal(err)
		}

		if n != len([]byte(line)) {
			b.Fatalf("Write() = %d, want %d", n, len([]byte(line)))
		}
	}
}
