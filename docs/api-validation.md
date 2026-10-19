# Actionable request validation

The beta publishing test exposed two confusing responses. The regressions first reproduced these behaviors through the global app:

- videoUrl="string" returned 422 with an empty fields object. Sermon validation now identifies videoUrl and explains the supported HTTPS YouTube format or the empty/omitted option.
- Response fields copied into PATCH (such as id or publishedAt) returned generic invalid_json. Unknown request fields now return 400 unknown_field with error.fields naming the unaccepted field. The resource ID remains in the URL; publishedAt is server-controlled.

Wrong JSON field types return 400 invalid_field_type with field guidance. Syntax errors, null objects and multiple JSON values retain invalid_json. Existing payload-size and content-type protections remain in place. Error responses do not echo field values, passwords, tokens, database errors or Go type details.

Sermon validation identifies invalid text, preachedAt, videoUrl, version, status and publication scheduling inputs. Publication errors still use 422 validation_failed. Version conflicts remain 409; authorization and storage failures are unchanged.

Swagger now provides explicit minimal draft, publish-only, edit-title and unpublish request examples. These exclude response-only fields and placeholder video URLs. Use the current version returned by editor detail when updating; do not blindly increment a stale version.
