-- Plannercore Schema v1.0

CREATE TABLE IF NOT EXISTS planner_plans (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    name TEXT NOT NULL,
    description TEXT,
    background_color TEXT DEFAULT '#1e293b',
    is_favorite BOOLEAN DEFAULT FALSE,
    is_template BOOLEAN DEFAULT FALSE,
    created_by TEXT,
    created_at TIMESTAMPTZ DEFAULT NOW(),
    updated_at TIMESTAMPTZ DEFAULT NOW(),
    archived_at TIMESTAMPTZ
);

CREATE TABLE IF NOT EXISTS planner_members (
    plan_id UUID REFERENCES planner_plans(id) ON DELETE CASCADE,
    user_id TEXT,
    role TEXT DEFAULT 'member',
    PRIMARY KEY (plan_id, user_id)
);

CREATE TABLE IF NOT EXISTS planner_buckets (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    plan_id UUID REFERENCES planner_plans(id) ON DELETE CASCADE,
    name TEXT NOT NULL,
    position DOUBLE PRECISION NOT NULL DEFAULT 0,
    color TEXT,
    created_at TIMESTAMPTZ DEFAULT NOW()
);

CREATE TABLE IF NOT EXISTS planner_tasks (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    plan_id UUID REFERENCES planner_plans(id) ON DELETE CASCADE,
    bucket_id UUID REFERENCES planner_buckets(id) ON DELETE SET NULL,
    title TEXT NOT NULL,
    rich_text_notes TEXT,
    priority TEXT DEFAULT 'medium',
    progress INT DEFAULT 0 CHECK (progress >= 0 AND progress <= 100),
    start_date TIMESTAMPTZ,
    due_date TIMESTAMPTZ,
    completed_at TIMESTAMPTZ,
    position DOUBLE PRECISION NOT NULL DEFAULT 0,
    checklist_completed_count INT DEFAULT 0,
    checklist_total_count INT DEFAULT 0,
    created_by TEXT,
    created_at TIMESTAMPTZ DEFAULT NOW(),
    updated_at TIMESTAMPTZ DEFAULT NOW()
);

CREATE TABLE IF NOT EXISTS planner_checklist_items (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    task_id UUID REFERENCES planner_tasks(id) ON DELETE CASCADE,
    title TEXT NOT NULL,
    is_completed BOOLEAN DEFAULT FALSE,
    position INT DEFAULT 0,
    completed_by TEXT,
    completed_at TIMESTAMPTZ,
    created_at TIMESTAMPTZ DEFAULT NOW()
);

CREATE TABLE IF NOT EXISTS planner_labels (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    plan_id UUID REFERENCES planner_plans(id) ON DELETE CASCADE,
    name TEXT NOT NULL,
    color TEXT NOT NULL
);

CREATE TABLE IF NOT EXISTS planner_task_labels (
    task_id UUID REFERENCES planner_tasks(id) ON DELETE CASCADE,
    label_id UUID REFERENCES planner_labels(id) ON DELETE CASCADE,
    PRIMARY KEY (task_id, label_id)
);

CREATE TABLE IF NOT EXISTS planner_task_assignees (
    task_id UUID REFERENCES planner_tasks(id) ON DELETE CASCADE,
    user_id TEXT,
    PRIMARY KEY (task_id, user_id)
);

CREATE TABLE IF NOT EXISTS planner_comments (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    task_id UUID REFERENCES planner_tasks(id) ON DELETE CASCADE,
    user_id TEXT,
    content TEXT NOT NULL,
    created_at TIMESTAMPTZ DEFAULT NOW()
);

CREATE TABLE IF NOT EXISTS planner_attachments (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    task_id UUID REFERENCES planner_tasks(id) ON DELETE CASCADE,
    filename TEXT NOT NULL,
    file_path TEXT NOT NULL,
    file_size BIGINT,
    mime_type TEXT,
    uploaded_by TEXT,
    created_at TIMESTAMPTZ DEFAULT NOW()
);

CREATE TABLE IF NOT EXISTS planner_dependencies (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    predecessor_id UUID REFERENCES planner_tasks(id) ON DELETE CASCADE,
    successor_id UUID REFERENCES planner_tasks(id) ON DELETE CASCADE,
    dependency_type TEXT DEFAULT 'finish-to-start',
    lag INTERVAL DEFAULT '0',
    UNIQUE(predecessor_id, successor_id)
);

CREATE TABLE IF NOT EXISTS planner_sprints (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    plan_id UUID REFERENCES planner_plans(id) ON DELETE CASCADE,
    name TEXT NOT NULL,
    goal TEXT,
    start_date TIMESTAMPTZ,
    end_date TIMESTAMPTZ,
    is_active BOOLEAN DEFAULT FALSE,
    created_at TIMESTAMPTZ DEFAULT NOW()
);

CREATE TABLE IF NOT EXISTS planner_sprint_tasks (
    sprint_id UUID REFERENCES planner_sprints(id) ON DELETE CASCADE,
    task_id UUID REFERENCES planner_tasks(id) ON DELETE CASCADE,
    PRIMARY KEY (sprint_id, task_id)
);

CREATE TABLE IF NOT EXISTS planner_goals (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    plan_id UUID REFERENCES planner_plans(id) ON DELETE CASCADE,
    parent_goal_id UUID REFERENCES planner_goals(id),
    title TEXT NOT NULL,
    description TEXT,
    progress INT DEFAULT 0,
    status TEXT DEFAULT 'not-started',
    created_at TIMESTAMPTZ DEFAULT NOW()
);

CREATE TABLE IF NOT EXISTS planner_custom_fields (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    plan_id UUID REFERENCES planner_plans(id) ON DELETE CASCADE,
    name TEXT NOT NULL,
    field_type TEXT NOT NULL,
    choices JSONB,
    position INT DEFAULT 0
);

CREATE TABLE IF NOT EXISTS planner_custom_field_values (
    field_id UUID REFERENCES planner_custom_fields(id) ON DELETE CASCADE,
    task_id UUID REFERENCES planner_tasks(id) ON DELETE CASCADE,
    value TEXT,
    PRIMARY KEY (field_id, task_id)
);

CREATE TABLE IF NOT EXISTS planner_baselines (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    plan_id UUID REFERENCES planner_plans(id) ON DELETE CASCADE,
    name TEXT NOT NULL,
    snapshot JSONB,
    created_at TIMESTAMPTZ DEFAULT NOW()
);

CREATE TABLE IF NOT EXISTS planner_task_history (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    task_id UUID REFERENCES planner_tasks(id) ON DELETE CASCADE,
    user_id TEXT,
    field_changed TEXT NOT NULL,
    old_value TEXT,
    new_value TEXT,
    changed_at TIMESTAMPTZ DEFAULT NOW()
);

CREATE TABLE IF NOT EXISTS planner_plan_links (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    plan_id UUID REFERENCES planner_plans(id) ON DELETE CASCADE,
    entity_type TEXT NOT NULL,
    entity_id UUID NOT NULL,
    entity_name TEXT,
    created_at TIMESTAMPTZ DEFAULT NOW()
);

CREATE TABLE IF NOT EXISTS planner_my_day (
    user_id TEXT,
    task_id UUID REFERENCES planner_tasks(id) ON DELETE CASCADE,
    added_at TIMESTAMPTZ DEFAULT NOW(),
    PRIMARY KEY (user_id, task_id)
);

-- Indexes
CREATE INDEX IF NOT EXISTS idx_planner_tasks_plan_bucket ON planner_tasks(plan_id, bucket_id);
CREATE INDEX IF NOT EXISTS idx_planner_tasks_due_date ON planner_tasks(due_date);
CREATE INDEX IF NOT EXISTS idx_planner_tasks_position ON planner_tasks(bucket_id, position);
CREATE INDEX IF NOT EXISTS idx_planner_task_assignees_user ON planner_task_assignees(user_id);
CREATE INDEX IF NOT EXISTS idx_planner_task_history_task ON planner_task_history(task_id);
CREATE INDEX IF NOT EXISTS idx_planner_plan_links_entity ON planner_plan_links(entity_type, entity_id);
CREATE INDEX IF NOT EXISTS idx_planner_my_day_user ON planner_my_day(user_id);

ALTER TABLE planner_tasks ADD COLUMN recurrence TEXT DEFAULT '';

