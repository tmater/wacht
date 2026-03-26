## Migration Policy

Until the first public release, migration history in this directory is still
mutable. It is acceptable to edit existing migration files in place, including
`000001_init.*.sql`, because there is not yet a supported upgrade path for
existing installations.

Reviewers should not require additive follow-up migrations purely for upgrade
safety during this pre-release phase.

After the first public release, applied migrations become immutable. Schema
changes must then be introduced through new forward migrations instead of
editing previously released files.
