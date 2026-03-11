package indicator

type Serie struct {
	Unix  int64
	Value float64
}

// Moving Average
type MA struct {
	period int
	values []float64
	sum    float64
}

func NewMA(period int) *MA {
	if period == 0 {
		panic("period of the moving average cannot be 0")
	}
	return &MA{
		period: period,
		values: make([]float64, period),
	}
}

func (ma *MA) Add(value float64, unix int64) Serie {
	if len(ma.values) == ma.period {
		oldest := ma.values[0]
		ma.values = ma.values[1:]
		ma.sum -= oldest
	}
	ma.values = append(ma.values, value)
	ma.sum += value
	return Serie{Unix: unix, Value: ma.Next()}
}

func (ma *MA) Next() float64 {
	if len(ma.values) == 0 {
		return 0
	}
	return ma.sum / float64(len(ma.values))
}

func (ma *MA) Period() int {
	return ma.period
}
