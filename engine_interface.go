package floxy

import (
	"context"
)

type IEngine interface {
	MakeHumanDecision(
		ctx context.Context,
		stepID int64,
		decidedBy string,
		decision HumanDecision,
		comment *string,
	) error
}
