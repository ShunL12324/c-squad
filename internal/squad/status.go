package squad

// DeliveryState tracks transport attempts independently of recipient acknowledgment.
// A sent message can be retried until the recipient confirms it.
type DeliveryState string

// Supported DeliveryState values.
const (
	DeliveryStateSuperseded     DeliveryState = "superseded"
	DeliveryStateAcknowledged   DeliveryState = "acknowledged"
	DeliveryStateNeedsAttention DeliveryState = "needs_attention"
	DeliveryStatePending        DeliveryState = "pending"
	DeliveryStateSending        DeliveryState = "sending"
	DeliveryStateSent           DeliveryState = "sent"
)

// DispatchMode controls whether master assigns a task or members may claim it.
type DispatchMode string

// Supported DispatchMode values.
const (
	DispatchModeAssigned DispatchMode = "assigned"
	DispatchModeOpen     DispatchMode = "open"
)

// MemberState records the observed availability of a native agent session.
// Native waiting reasons may extend the waiting_ prefix; this is not task completion.
type MemberState string

// Supported MemberState values.
const (
	MemberStateCrashed        MemberState = "crashed"
	MemberStateError          MemberState = "error"
	MemberStateIdle           MemberState = "idle"
	MemberStateNeedsAttention MemberState = "needs_attention"
	MemberStateRemoved        MemberState = "removed"
	MemberStateStarting       MemberState = "starting"
	MemberStateStopped        MemberState = "stopped"
	MemberStateStopping       MemberState = "stopping"
	MemberStateWaitingMaster  MemberState = "waiting_master"
	MemberStateWorking        MemberState = "working"
)

// MilestoneState records checkpoint reporting and any master approval gate.
type MilestoneState string

// Supported MilestoneState values.
const (
	MilestoneStateApproved         MilestoneState = "approved"
	MilestoneStateAwaitingApproval MilestoneState = "awaiting_approval"
	MilestoneStatePending          MilestoneState = "pending"
	MilestoneStateReported         MilestoneState = "reported"
)

// QuestionState records whether master has answered a blocking escalation.
type QuestionState string

// Supported QuestionState values.
const (
	QuestionStateAnswered QuestionState = "answered"
	QuestionStateOpen     QuestionState = "open"
)

// TaskPhase records progress through delivery; blockers are tracked separately.
// Blocked is retained only to migrate ledgers written by the original prototype.
type TaskPhase string

// Supported TaskPhase values.
const (
	TaskPhaseAwaitingMerge TaskPhase = "awaiting_merge"
	TaskPhaseBlocked       TaskPhase = "blocked"
	TaskPhaseDone          TaskPhase = "done"
	TaskPhaseInProgress    TaskPhase = "in_progress"
	TaskPhaseInReview      TaskPhase = "in_review"
	TaskPhaseMerging       TaskPhase = "merging"
	TaskPhasePreparing     TaskPhase = "preparing"
	TaskPhaseReady         TaskPhase = "ready"
)

// TeamPhase records startup, shutdown, and cleanup progress for a team incarnation.
type TeamPhase string

// Supported TeamPhase values.
const (
	TeamPhaseCleanupFailed TeamPhase = "cleanup_failed"
	TeamPhaseInterrupted   TeamPhase = "interrupted"
	TeamPhaseRunning       TeamPhase = "running"
	TeamPhaseStarting      TeamPhase = "starting"
	TeamPhaseStopped       TeamPhase = "stopped"
	TeamPhaseStopping      TeamPhase = "stopping"
)

// EvidenceKind distinguishes independent review from test verification.
type EvidenceKind string

// Supported evidence kinds; both are required by the code-task merge policy.
const (
	EvidenceReview EvidenceKind = "review"
	EvidenceTest   EvidenceKind = "test"
)
