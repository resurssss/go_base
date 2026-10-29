package domain

import (
	"errors"
	"fmt"
)

// Sentinel-ошибки (п.4 ТЗ). Оборачиваются в другие ошибки через fmt.Errorf
// с глаголом %w, поэтому errors.Is находит их в любой точке цепочки
// оборачивания, даже через несколько уровней вызова (например, из
// HTTP-хендлера сквозь service-слой сквозь entity-слой).
var (
	ErrValidation = errors.New("validation error")
	ErrNotFound   = errors.New("resource not found")
	// ErrConflict используется только как "маркер" для errors.Is со стороны
	// вызывающего кода, который не хочет проверять конкретный тип
	// SlotConflictError, а просто хочет знать "это конфликт записи или нет".
	ErrConflict = errors.New("booking conflict")
)

// SlotConflictError
type SlotConflictError struct {
	SlotID string
	From   string
	To     string
}

func (e *SlotConflictError) Error() string {
	return fmt.Sprintf("slot %s is not available from %s to %s", e.SlotID, e.From, e.To)
}

// Is позволяет errors.Is(err, domain.ErrConflict) находить SlotConflictError,
// не зная его конкретного типа — своего рода "теги" для ошибок.  --- ищет конкретное значение ошибки
func (e *SlotConflictError) Is(target error) bool {
	return target == ErrConflict
}
