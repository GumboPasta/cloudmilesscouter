CREATE TABLE airlines (
    id   SERIAL PRIMARY KEY,
    code TEXT NOT NULL UNIQUE,
    name TEXT NOT NULL
);

CREATE TABLE routes (
    id          SERIAL PRIMARY KEY,
    origin      TEXT NOT NULL,
    destination TEXT NOT NULL,
    UNIQUE (origin, destination)
);

CREATE TABLE cabins (
    id   SERIAL PRIMARY KEY,
    name TEXT NOT NULL UNIQUE
);

INSERT INTO cabins (name) VALUES
    ('economy'),
    ('premium_economy'),
    ('business'),
    ('first');

CREATE TABLE awards (
    id                  SERIAL PRIMARY KEY,
    airline_id          INTEGER NOT NULL REFERENCES airlines (id),
    route_id            INTEGER NOT NULL REFERENCES routes (id),
    cabin_id            INTEGER NOT NULL REFERENCES cabins (id),
    search_date         DATE NOT NULL,
    scraped_at          TIMESTAMPTZ NOT NULL,
    flight_number       TEXT NOT NULL,
    flight_origin       TEXT NOT NULL,
    flight_destination  TEXT NOT NULL,
    depart_time         TIMESTAMP NOT NULL,
    arrive_time         TIMESTAMP NOT NULL,
    duration_minutes    INTEGER NOT NULL,
    stops               INTEGER NOT NULL,
    award_type          TEXT NOT NULL,
    points_cost         INTEGER NOT NULL,
    taxes_fees          NUMERIC(10, 2) NOT NULL,
    currency            TEXT NOT NULL DEFAULT 'USD',
    created_at          TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE INDEX awards_route_date_idx ON awards (route_id, search_date);
