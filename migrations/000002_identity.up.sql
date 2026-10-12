CREATE TABLE flashdrop.user_credentials (
    user_id UUID PRIMARY KEY,
    email TEXT NOT NULL,
    password_hash TEXT NOT NULL,

    CONSTRAINT fk_user_credentials_user
        FOREIGN KEY (user_id)
        REFERENCES flashdrop.users(id),

    CONSTRAINT uq_user_credentials_email
        UNIQUE(email),

    CONSTRAINT ck_user_credentials_email_normalized
        CHECK (
            email <> ''
            AND email = lower(btrim(email))
        ),

    CONSTRAINT ck_user_credentials_hash_not_empty
        CHECK (password_hash <> '')
);