# DevBoard Quota Activator

This is the independent, outbound scheduler for an existing DevBoard Hub. It
does not expose a web port. The Hub Admin page remains responsible for saving
settings and showing audit/status; this service owns schedule execution and
manual-test requests.

Its `DEVBOARD_HUB_DATA_DIR` must be the existing Hub `data` directory. It
must use the same UID/GID as the Hub because it needs to read the private
configuration and provider key written from Hub Admin.

Deploy this bundle separately from the Hub bundle. Updating the Hub does not
restart this service, and updating this service does not recreate the Hub.
