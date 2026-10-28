package app

import "time"

type User struct {
	ID                 uint64         `json:"id"`
	Username           string         `json:"username"`
	DisplayName        string         `json:"display_name"`
	PasswordHash       string         `json:"-"`
	Enabled            bool           `json:"enabled"`
	MustChangePassword bool           `json:"must_change_password"`
	Preferences        map[string]any `json:"preferences" gorm:"serializer:json;type:jsonb"`
	CreatedAt          time.Time      `json:"created_at"`
	Roles              []Role         `json:"roles" gorm:"many2many:user_roles"`
}
type Scope struct {
	AccountID  uint64 `json:"account_id"`
	Kind       string `json:"kind"`
	Region     string `json:"region"`
	InstanceID string `json:"instance_id"`
}
type Role struct {
	ID            uint64   `json:"id"`
	Name          string   `json:"name"`
	Permissions   []string `json:"permissions" gorm:"serializer:json;type:jsonb"`
	Scopes        []Scope  `json:"scopes" gorm:"serializer:json;type:jsonb"`
	Administrator bool     `json:"administrator"`
}
type Session struct {
	ID        string    `json:"-"`
	UserID    uint64    `json:"user_id"`
	CSRF      string    `json:"-"`
	CreatedAt time.Time `json:"created_at"`
	LastSeen  time.Time `json:"last_seen"`
}
type CloudAccount struct {
	ID        uint64     `json:"id"`
	Name      string     `json:"name"`
	Provider  string     `json:"provider" gorm:"default:tencent"`
	Site      string     `json:"site" gorm:"default:china"`
	SecretID  string     `json:"-"`
	SecretKey string     `json:"-"`
	Regions   []string   `json:"regions" gorm:"serializer:json;type:jsonb"`
	Enabled   bool       `json:"enabled"`
	LastSync  *time.Time `json:"last_sync"`
	SyncError string     `json:"sync_error"`
}
type Metric struct {
	Value     *float64   `json:"value"`
	Unit      string     `json:"unit"`
	SampledAt *time.Time `json:"sampled_at"`
	Status    string     `json:"status"`
	Reason    string     `json:"reason,omitempty"`
	Mount     string     `json:"mount,omitempty"`
}
type Resource struct {
	ID              uint64            `json:"id"`
	AccountID       uint64            `json:"account_id"`
	Provider        string            `json:"provider"`
	Kind            string            `json:"kind"`
	Region          string            `json:"region"`
	InstanceID      string            `json:"instance_id"`
	Name            string            `json:"name"`
	PublicIP        string            `json:"public_ip"`
	PrivateIP       string            `json:"private_ip"`
	Spec            string            `json:"spec"`
	CPU             int               `json:"cpu"`
	Memory          int               `json:"memory"`
	Disk            int               `json:"disk"`
	State           string            `json:"state"`
	Tags            map[string]string `json:"tags" gorm:"serializer:json;type:jsonb"`
	TagsError       string            `json:"tags_error"`
	Metrics         map[string]Metric `json:"metrics" gorm:"serializer:json;type:jsonb"`
	SyncedAt        time.Time         `json:"synced_at"`
	AccountName     string            `json:"account_name" gorm:"-"`
	AllowedActions  []string          `json:"allowed_actions" gorm:"-"`
	OperationStatus string            `json:"operation_status" gorm:"-"`
}

func isServer(kind string) bool {
	return kind == "cvm" || kind == "lighthouse" || kind == "ecs" || kind == "sas"
}

type Audit struct {
	ID        uint64    `json:"id"`
	UserID    *uint64   `json:"user_id"`
	Actor     string    `json:"actor"`
	SourceIP  string    `json:"source_ip"`
	Action    string    `json:"action"`
	Target    string    `json:"target"`
	Result    string    `json:"result"`
	Message   string    `json:"message"`
	RequestID string    `json:"request_id"`
	CreatedAt time.Time `json:"created_at"`
}
type Task struct {
	ID                 uint64     `json:"id"`
	ResourceID         *uint64    `json:"resource_id"`
	AccountID          *uint64    `json:"account_id"`
	AuditID            *uint64    `json:"audit_id"`
	Action             string     `json:"action"`
	Status             string     `json:"status"`
	RequestID          string     `json:"request_id"`
	Error              string     `json:"error"`
	CreatedAt          time.Time  `json:"created_at"`
	UpdatedAt          time.Time  `json:"updated_at"`
	NextRun            time.Time  `json:"next_run"`
	LeaseUntil         *time.Time `json:"-"`
	BaselineState      string     `json:"-"`
	ObservedTransition bool       `json:"-"`
}
type Settings struct {
	ID             int      `json:"-"`
	RefreshSeconds int      `json:"refresh_seconds"`
	IdleMinutes    int      `json:"idle_minutes"`
	MaxHours       int      `json:"max_hours"`
	RetentionDays  int      `json:"retention_days"`
	Metrics        []string `json:"metrics" gorm:"serializer:json;type:jsonb"`
}
type OAuthProvider struct {
	ID           uint64 `json:"id"`
	Name         string `json:"name"`
	ClientID     string `json:"client_id"`
	ClientSecret string `json:"-"`
	AuthURL      string `json:"auth_url"`
	TokenURL     string `json:"token_url"`
	UserURL      string `json:"user_url"`
	RedirectURL  string `json:"redirect_url"`
	Scopes       string `json:"scopes"`
	SubjectField string `json:"subject_field"`
	Enabled      bool   `json:"enabled"`
}
type Identity struct {
	ID         uint64 `json:"id"`
	ProviderID uint64 `json:"provider_id"`
	Subject    string `json:"subject"`
	UserID     uint64 `json:"user_id"`
}
type OAuthState struct {
	ID         string `gorm:"primaryKey"`
	ProviderID uint64
	UserID     *uint64
	SessionID  string
	Verifier   string
	ExpiresAt  time.Time
}
type KeyCheck struct {
	ID    int
	Value string
}

func (OAuthProvider) TableName() string { return "oauth_providers" }
func (OAuthState) TableName() string    { return "oauth_states" }
