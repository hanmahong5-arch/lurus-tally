package intake

import (
	"fmt"

	"github.com/google/uuid"

	domain "github.com/hanmahong5-arch/lurus-tally/internal/domain/intake"
)

const (
	CodeDuplicate = domain.CodeDuplicate
	CodeExists    = domain.CodeExists
	CodeLocked    = domain.CodeLocked
	CodeMoney     = domain.CodeMoney
)

var (
	ErrValidation = domain.ErrValidation
	ErrNotFound   = domain.ErrNotFound
	ErrDuplicate  = domain.ErrDuplicate
	ErrLocked     = domain.ErrLocked
	ErrMoney      = domain.ErrMoney
	ErrConflict   = domain.ErrConflict
)

type Duplicate = domain.Duplicate

func dupIntake(id uuid.UUID, day, message, code string) *Duplicate {
	return &Duplicate{ExistingID: id, Kind: "intake", Code: code, IssueDate: day, Message: message}
}

func bookedMessage(day string, booked bool) string {
	when := ""
	if len(day) >= 10 {
		when = fmt.Sprintf("已在%s年%s月%s日", day[0:4], trimZero(day[5:7]), trimZero(day[8:10]))
	}
	if booked {
		if when == "" {
			return "这张票已经入过账"
		}
		return "这张票" + when + "入账"
	}
	if when == "" {
		return "这张票已经在待核对里"
	}
	return "这张票" + when + "收过，正在待核对"
}

func trimZero(s string) string {
	if len(s) == 2 && s[0] == '0' {
		return s[1:]
	}
	return s
}
