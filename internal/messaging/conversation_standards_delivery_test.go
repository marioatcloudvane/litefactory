package messaging

import "context"

type standardsDeliverySender struct {
	targets []Target
	texts   []string
	result  Delivery
}

func (f *standardsDeliverySender) Deliver(_ context.Context, target Target, text string) Delivery {
	f.targets = append(f.targets, target)
	f.texts = append(f.texts, text)
	return f.result
}
