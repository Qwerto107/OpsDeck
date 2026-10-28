export interface Scope {
  account_id: number;
  kind: string;
  region: string;
  instance_id: string;
}
export interface Role {
  id: number;
  name: string;
  permissions: string[];
  scopes: Scope[];
  administrator: boolean;
}
export interface User {
  id: number;
  username: string;
  display_name: string;
  enabled: boolean;
  must_change_password: boolean;
  roles: Role[];
  preferences: Record<string, unknown>;
}
export interface Metric {
  value: number | null;
  unit: string;
  sampled_at: string | null;
  status: string;
  reason?: string;
  mount?: string;
}
export interface Resource {
  operation_status?: string;
  id: number;
  account_id: number;
  account_name: string;
  provider: string;
  kind: string;
  region: string;
  instance_id: string;
  name: string;
  public_ip: string;
  private_ip: string;
  spec: string;
  cpu: number;
  memory: number;
  disk: number;
  state: string;
  tags: Record<string, string>;
  tags_error: string;
  metrics: Record<string, Metric>;
  synced_at: string;
  allowed_actions: string[];
}
export interface Account {
  site: string;
  id: number;
  name: string;
  provider: string;
  regions: string[];
  enabled: boolean;
  last_sync: string | null;
  sync_error: string;
}
export interface Settings {
  refresh_seconds: number;
  idle_minutes: number;
  max_hours: number;
  retention_days: number;
  metrics: string[];
}
export interface SortRule {
  field: "name" | "region" | "tag";
  key?: string;
  desc: boolean;
}
export interface Task {
  id: number;
  action: string;
  status: string;
  request_id: string;
  error: string;
  created_at: string;
  updated_at: string;
}
export interface Audit {
  id: number;
  actor: string;
  source_ip: string;
  action: string;
  target: string;
  result: string;
  message: string;
  request_id: string;
  created_at: string;
}
export interface Provider {
  id: number;
  name: string;
  client_id: string;
  auth_url: string;
  token_url: string;
  user_url: string;
  redirect_url: string;
  scopes: string;
  subject_field: string;
  enabled: boolean;
}
export interface Identity {
  id: number;
  user_id: number;
  provider_id: number;
  subject: string;
}
export interface Page<T> {
  cache?: ResourceCache;
  items: T[];
  total: number;
}

export interface ResourceCache {
  updated_at: string | null;
  ttl_seconds: number;
}
