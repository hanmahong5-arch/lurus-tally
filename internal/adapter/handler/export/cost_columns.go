package export

import (
	"encoding/csv"
	"io"
	"sync"
)

// stockCostColumns are the stock.csv columns only people with cost.view get.
var stockCostColumns = []string{"单位成本", "库存金额"}

// csvColumnFilter is an io.WriteCloser that re-emits the CSV written to it
// without the columns whose header is in drop. The export use cases live in the
// app layer and write their full sheet; trimming here keeps one place deciding
// who sees cost.
type csvColumnFilter struct {
	pw   *io.PipeWriter
	done chan struct{}
	mu   sync.Mutex
	err  error
}

func newCSVColumnFilter(dst io.Writer, drop ...string) *csvColumnFilter {
	pr, pw := io.Pipe()
	f := &csvColumnFilter{pw: pw, done: make(chan struct{})}
	dropSet := make(map[string]bool, len(drop))
	for _, d := range drop {
		dropSet[d] = true
	}
	go func() {
		defer close(f.done)
		err := filterCSV(pr, dst, dropSet)
		_ = pr.CloseWithError(err) // unblock a writer if we stopped early
		f.mu.Lock()
		f.err = err
		f.mu.Unlock()
	}()
	return f
}

func (f *csvColumnFilter) Write(p []byte) (int, error) { return f.pw.Write(p) }

// Close flushes the filter and returns the first error it met.
func (f *csvColumnFilter) Close() error {
	_ = f.pw.Close()
	<-f.done
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.err
}

func filterCSV(src io.Reader, dst io.Writer, drop map[string]bool) error {
	cr := csv.NewReader(src)
	cr.FieldsPerRecord = -1
	cw := csv.NewWriter(dst)
	var keep []int
	first := true
	for {
		rec, err := cr.Read()
		if err == io.EOF {
			break
		}
		if err != nil {
			return err
		}
		if first {
			first = false
			for i, h := range rec {
				if !drop[h] {
					keep = append(keep, i)
				}
			}
		}
		out := make([]string, 0, len(keep))
		for _, i := range keep {
			if i < len(rec) {
				out = append(out, rec[i])
			} else {
				out = append(out, "")
			}
		}
		if err := cw.Write(out); err != nil {
			return err
		}
	}
	cw.Flush()
	return cw.Error()
}
