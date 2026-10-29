package entity

import (
	"fmt"
	"time"
)

// В entity сущности и правила их поведения: интерфейс Bookable + две реализации (GroupClass, PersonalTraining),
// у каждой своя логика Book/IsAvailable.
// Отвечает за «что значит успешно записаться на конкретный тип слота».

// Bookable — групповое занятие или персональная тренировка в фитнес-клубе.
type Bookable interface {
	// Book пытается записать client на [from, to).
	// Меняет состояние -> в реализациях используется pointer receiver
	Book(client string, from, to time.Time) error
	// IsAvailable только проверяет доступность, состояние не меняет.
	IsAvailable(from, to time.Time) bool
	// SlotID возвращает идентификатор сущности для использования как ключ в service-слое.
	SlotID() string
}

// BaseEntity — обобщённая сущность системы: общие для всех типов
// записей (групповое занятие, персональная тренировка) поля — идентификатор, время создания записи в системе.
type BaseEntity struct {
	ID        string
	CreatedAt time.Time
}

// Info — метод BaseEntity с value receiver: только читает поля
func (b BaseEntity) Info() string {
	return fmt.Sprintf("id=%s created=%s", b.ID, b.CreatedAt.Format(time.RFC3339))
}
