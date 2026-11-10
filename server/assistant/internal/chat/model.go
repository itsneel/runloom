package chat

import "time"

const UnconfiguredReply = "LLM is not configured."

// A bounded aggregate makes message + job creation atomic in both databases,
// including standalone MongoDB. Revisions fence concurrent writers and workers.
type Scope struct {
	User     string `json:"-"`
	Org      string `json:"org_id"`
	Team     string `json:"team_id"`
	Workflow string `json:"workflow_id,omitempty"`
}
type Message struct {
	ID        string    `json:"id"`
	Role      string    `json:"role"`
	Content   string    `json:"content"`
	JobID     string    `json:"job_id,omitempty"`
	CreatedAt time.Time `json:"created_at"`
}
type Job struct {
	SaveDraft    bool              `json:"save_draft,omitempty"`
	Context      *AuthoringContext `json:"authoring_context,omitempty"`
	Proposal     *Proposal         `json:"proposal,omitempty"`
	ProposalHash string            `json:"proposal_hash,omitempty"`
	Reverted     *ApplyReceipt     `json:"reverted,omitempty"`
	Applied      *ApplyReceipt     `json:"applied,omitempty"`
	ID           string            `json:"id"`
	RequestID    string            `json:"request_id"`
	Mode         string            `json:"mode,omitempty"`
	Kind         string            `json:"kind"`
	Status       string            `json:"status"`
	Error        string            `json:"error,omitempty"`
	Model        string            `json:"model"`
	CreatedAt    time.Time         `json:"created_at"`
	UpdatedAt    time.Time         `json:"updated_at"`
	Deadline     time.Time         `json:"deadline,omitempty"`
}

func (j Job) Active() bool { return j.Status == "queued" || j.Status == "running" }

type Conversation struct {
	ID        string    `json:"id"`
	Scope     Scope     `json:"scope"`
	Title     string    `json:"title"`
	Revision  int64     `json:"revision"`
	Messages  []Message `json:"messages"`
	Jobs      []Job     `json:"jobs"`
	CreatedAt time.Time `json:"created_at"`
	UpdatedAt time.Time `json:"updated_at"`
}

func (c Conversation) ActiveIndex() int {
	for i, j := range c.Jobs {
		if j.Active() {
			return i
		}
	}
	return -1
}
func (c Conversation) Active() bool { return c.ActiveIndex() >= 0 }
