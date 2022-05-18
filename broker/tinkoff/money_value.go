package tinkoff

type money interface {
	GetUnits() int64
	GetNano() int32
}

type moneyValue struct {
	money
}

func newMoneyValue(v money) *moneyValue {
	return &moneyValue{
		money: v,
	}
}

func (v *moneyValue) ToFloat() float64 {
	return float64(v.GetUnits()) + float64(v.GetNano())/10e8
}
