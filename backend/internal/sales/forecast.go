package sales

import "errors"

var (
	ErrCurrencyMismatch   = errors.New("forecast currency mismatch")
	ErrInvalidProbability = errors.New("invalid forecast probability")
)

type ForecastInput struct {
	Amount      Money
	Probability uint8
	Category    ForecastCategory
}

type Forecast struct {
	WeightedRevenue  Money
	CommittedRevenue Money
}

func CalculateForecast(inputs []ForecastInput) (Forecast, error) {
	if len(inputs) == 0 {
		return Forecast{}, nil
	}
	currency := inputs[0].Amount.Currency
	result := Forecast{
		WeightedRevenue:  Money{Currency: currency},
		CommittedRevenue: Money{Currency: currency},
	}
	for _, input := range inputs {
		if input.Amount.Currency != currency {
			return Forecast{}, ErrCurrencyMismatch
		}
		if input.Probability > 100 || input.Amount.Minor < 0 {
			return Forecast{}, ErrInvalidProbability
		}
		switch input.Category {
		case ClosedLost:
			continue
		case Committed, ClosedWon:
			result.WeightedRevenue.Minor += input.Amount.Minor
			result.CommittedRevenue.Minor += input.Amount.Minor
		default:
			result.WeightedRevenue.Minor += input.Amount.Minor * int64(input.Probability) / 100
		}
	}
	return result, nil
}
