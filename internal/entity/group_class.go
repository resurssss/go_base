package entity

import (
	"fmt"
	"time"

	"lab1/internal/domain"
)

// GroupClass — групповое занятие (йога, зумба и т.п.) с фиксированным расписанием и ограниченной вместимостью зала.
type GroupClass struct {
	BaseEntity // embedding вместо наследования (GroupClass содержит BaseEntity, а не является им) - автоматическое продвижение полей
	Name       string
	Trainer    string
	From       time.Time
	To         time.Time
	Capacity   int
	Price      domain.Money
	clients    []string
}

// NewGroupClass — конструктор вида NewX(...) (X, error).
// Возвращает *GroupClass (указатель)
func NewGroupClass(id, name, trainer string, from, to time.Time, capacity int, price domain.Money) (*GroupClass, error) {
	if name == "" {
		return nil, fmt.Errorf("%w: class name must not be empty", domain.ErrValidation)
	}
	if trainer == "" {
		return nil, fmt.Errorf("%w: trainer must not be empty", domain.ErrValidation)
	}
	if !from.Before(to) {
		return nil, fmt.Errorf("%w: from must be before to", domain.ErrValidation)
	}
	if capacity < 1 {
		return nil, fmt.Errorf("%w: capacity must be >= 1, got %d", domain.ErrValidation, capacity)
	}
	return &GroupClass{
		BaseEntity: BaseEntity{ID: id, CreatedAt: time.Now()},
		Name:       name,
		Trainer:    trainer,
		From:       from,
		To:         to,
		Capacity:   capacity,
		Price:      price,
	}, nil
}

func (c *GroupClass) SlotID() string { return c.ID }

// IsAvailable — не меняет состояние, receiver — pointer.
// У группового занятия расписание фиксировано заранее (в отличие от
// персональной тренировки, время которой выбирает клиент).
// Поэтому, в отличие от PersonalTraining.IsAvailable, аргументы from/to здесь намеренно не участвуют в проверке
func (c *GroupClass) IsAvailable(from, to time.Time) bool {
	return len(c.clients) < c.Capacity
}

// Book — pointer receiver: меняет состояние (добавляет клиента в срез).
func (c *GroupClass) Book(client string, from, to time.Time) error {
	if client == "" {
		return fmt.Errorf("%w: client name must not be empty", domain.ErrValidation)
	}
	if !from.Equal(c.From) || !to.Equal(c.To) {
		return fmt.Errorf("%w: class %s is scheduled from %s to %s, not the requested time",
			domain.ErrValidation, c.ID, c.From.Format(time.RFC3339), c.To.Format(time.RFC3339))
	}
	for _, existing := range c.clients {
		if existing == client {
			return &domain.SlotConflictError{
				SlotID: c.ID,
				From:   from.Format(time.RFC3339),
				To:     to.Format(time.RFC3339),
			}
		}
	}
	if len(c.clients) >= c.Capacity {
		return &domain.SlotConflictError{
			SlotID: c.ID,
			From:   from.Format(time.RFC3339),
			To:     to.Format(time.RFC3339),
		}
	}
	c.clients = append(c.clients, client)
	return nil
}
