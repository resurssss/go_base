package domain

import "fmt"

// В domain находятся базовые типы и ошибки предметной области

// ClassStatus — именованный тип-перечисление на iota (п.1 ТЗ). Отражает
// текущее состояние группового занятия в расписании фитнес-клуба.
type ClassStatus int

const (
	StatusScheduled ClassStatus = iota
	StatusFull
	StatusCancelled
)

// String делает ClassStatus читаемым при логировании / сериализации.
func (s ClassStatus) String() string {
	switch s {
	case StatusScheduled:
		return "scheduled"
	case StatusFull:
		return "full"
	case StatusCancelled:
		return "cancelled"
	default:
		return "unknown"
	}
}

type Money struct {
	AmountCents int64
	Currency    string
}

// NewMoney — функция-конструктор вида NewX(...) (X, error), валидирует вход.
func NewMoney(amountCents int64, currency string) (Money, error) {
	if amountCents < 0 {
		return Money{}, fmt.Errorf("%w: amount must be non-negative, got %d", ErrValidation, amountCents)
	}
	if currency == "" {
		return Money{}, fmt.Errorf("%w: currency must not be empty", ErrValidation)
	}
	return Money{AmountCents: amountCents, Currency: currency}, nil
}

func (m Money) String() string {
	return fmt.Sprintf("%d.%02d %s", m.AmountCents/100, m.AmountCents%100, m.Currency)
}
