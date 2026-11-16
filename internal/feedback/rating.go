package feedback

import (
	"errors"
	"strings"
)

type Rating struct {
	RequestID string `json:"request_id"`
	Score     int    `json:"score"`
}

func (rating Rating) Validate() error {
	switch {
	case strings.TrimSpace(rating.RequestID) == "":
		return errors.New("Ошибка: идентификатор запроса")
	case rating.Score < 1 || rating.Score > 5:
		return errors.New("Ошибка: оценка выходит за границу определения")
	}
	return nil

}
