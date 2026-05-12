package approve

import (
	"time"

	agent "github.com/cago-frame/agents/agent"
)

// Hook returns a tool middleware that submits the call to the Approver's queue
// and blocks on the decision. The middleware respects the dispatch context — if
// it is canceled while waiting, the middleware aborts with the ctx error.
//
// On approval, the middleware calls c.Next() to invoke the rest of the chain
// (and ultimately the tool). On denial, it short-circuits with c.AbortWithDeny.
func Hook(a *Approver) agent.ToolMiddleware {
	return func(c *agent.ToolContext) {
		p := Pending{
			ID:          newID(),
			ToolName:    c.ToolName,
			ToolUseID:   c.ToolUseID,
			Input:       c.Input,
			SubmittedAt: time.Now(),
		}

		type result struct {
			d   decision
			err error
		}
		ch := make(chan result, 1)
		go func() {
			d, err := a.submit(p)
			ch <- result{d, err}
		}()

		ctx := c.Context()
		select {
		case <-ctx.Done():
			_ = a.Deny(p.ID, "context canceled")
			c.AbortWithError(ctx.Err())
			return
		case r := <-ch:
			if r.err != nil {
				c.AbortWithError(r.err)
				return
			}
			if r.d.approve {
				c.Next()
				return
			}
			c.AbortWithDeny(r.d.reason)
		}
	}
}
