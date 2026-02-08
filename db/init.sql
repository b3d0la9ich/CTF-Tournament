CREATE TABLE IF NOT EXISTS users (
  id           SERIAL PRIMARY KEY,
  username     TEXT UNIQUE NOT NULL,
  pass_hash    TEXT NOT NULL,
  role         TEXT NOT NULL CHECK (role IN ('admin','user')),
  points       INT NOT NULL DEFAULT 0,
  created_at   TIMESTAMP NOT NULL DEFAULT now()
);

CREATE TABLE IF NOT EXISTS logs (
  id          BIGSERIAL PRIMARY KEY,
  created_at  TIMESTAMP NOT NULL DEFAULT now(),
  actor_id    INT NULL REFERENCES users(id) ON DELETE SET NULL,
  action      TEXT NOT NULL,
  details     TEXT NOT NULL DEFAULT ''
);

CREATE TABLE IF NOT EXISTS matches (
  id             SERIAL PRIMARY KEY,
  title          TEXT NOT NULL,
  mode           TEXT NOT NULL CHECK (mode IN ('solo','team')),
  starts_at      TIMESTAMP NULL,
  ends_at        TIMESTAMP NULL,
  status         TEXT NOT NULL DEFAULT 'open' CHECK (status IN ('open','closed','finished')),
  created_by     INT NOT NULL REFERENCES users(id),
  winner_user_id INT NULL REFERENCES users(id),
  winner_team_id INT NULL,
  created_at     TIMESTAMP NOT NULL DEFAULT now()
);

CREATE TABLE IF NOT EXISTS teams (
  id         SERIAL PRIMARY KEY,
  name       TEXT NOT NULL,
  owner_id   INT NOT NULL REFERENCES users(id),
  is_open    BOOLEAN NOT NULL DEFAULT TRUE,
  created_at TIMESTAMP NOT NULL DEFAULT now()
);

CREATE TABLE IF NOT EXISTS team_members (
  team_id INT NOT NULL REFERENCES teams(id) ON DELETE CASCADE,
  user_id INT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
  PRIMARY KEY(team_id, user_id)
);

CREATE TABLE IF NOT EXISTS applications (
  id         SERIAL PRIMARY KEY,
  match_id   INT NOT NULL REFERENCES matches(id) ON DELETE CASCADE,
  user_id    INT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
  team_id    INT NULL REFERENCES teams(id) ON DELETE SET NULL,
  status     TEXT NOT NULL DEFAULT 'pending' CHECK (status IN ('pending','approved','rejected')),
  created_at TIMESTAMP NOT NULL DEFAULT now(),
  UNIQUE(match_id, user_id)
);

CREATE TABLE IF NOT EXISTS match_participants (
  match_id INT NOT NULL REFERENCES matches(id) ON DELETE CASCADE,
  user_id  INT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
  team_id  INT NULL REFERENCES teams(id) ON DELETE SET NULL,
  PRIMARY KEY(match_id, user_id)
);

-- admin123 (bcrypt)
INSERT INTO users (username, pass_hash, role, points)
VALUES (
  'admin',
  '$2b$10$dSJFyztEwgw4JQDsleIGTOZDPxI3jYenHtWxg9GqwdCOy34RDg7Ny',
  'admin',
  0
)
ON CONFLICT (username) DO NOTHING;

INSERT INTO logs(actor_id, action, details)
SELECT id, 'seed_admin', 'Admin user created (username=admin)' FROM users WHERE username='admin';
