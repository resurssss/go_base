package entity

import (
	"fmt"
	"time"

	"lab1/internal/domain"
)

// timeRange — вспомогательный тип для хранения одной записи на персональную тренировку
type timeRange struct {
	from, to time.Time
	client   string
}

// PersonalTraining — персональная тренировка с тренером: в отличие от GroupClass,
// время выбирает клиент, слот эксклюзивный и есть минимальная длительность сессии
type PersonalTraining struct {
	BaseEntity
	Trainer            string
	Price              domain.Money
	MinDurationMinutes int
	bookings           []timeRange
}

// конструктор вида NewX(...) (X, error)
func NewPersonalTraining(id, trainer string, price domain.Money, minDurationMinutes int) (*PersonalTraining, error) {
	if trainer == "" {
		return nil, fmt.Errorf("%w: trainer must not be empty", domain.ErrValidation)
	}
	if minDurationMinutes < 1 {
		return nil, fmt.Errorf("%w: minDurationMinutes must be >= 1, got %d", domain.ErrValidation, minDurationMinutes)
	}
	return &PersonalTraining{
		BaseEntity:         BaseEntity{ID: id, CreatedAt: time.Now()},
		Trainer:            trainer,
		Price:              price,
		MinDurationMinutes: minDurationMinutes,
	}, nil
}

func (p *PersonalTraining) SlotID() string { return p.ID }

func (p *PersonalTraining) IsAvailable(from, to time.Time) bool {
	for _, b := range p.bookings {
		if from.Before(b.to) && b.from.Before(to) {
			return false
		}
	}
	return true
}

// Book — проявление полиморфизма (см Book в group_class)
func (p *PersonalTraining) Book(client string, from, to time.Time) error {
	if client == "" {
		return fmt.Errorf("%w: client name must not be empty", domain.ErrValidation)
	}
	if !from.Before(to) {
		return fmt.Errorf("%w: from must be before to", domain.ErrValidation)
	}
	duration := to.Sub(from).Minutes()
	if int(duration) < p.MinDurationMinutes {
		return fmt.Errorf("%w: personal training requires at least %d minutes, got %d",
			domain.ErrValidation, p.MinDurationMinutes, int(duration))
	}
	if !p.IsAvailable(from, to) {
		return &domain.SlotConflictError{
			SlotID: p.ID,
			From:   from.Format(time.RFC3339),
			To:     to.Format(time.RFC3339),
		}
	}
	p.bookings = append(p.bookings, timeRange{from: from, to: to, client: client})
	return nil
}
