# notifbuddy

**Note: project is currently experimental**, any feedback is greatly appreciated. Join the
[community slack](https://join.slack.com/t/notifbuddyworkspace/shared_invite/zt-43vft932a-AHlUb6eK5ssnJMBIpkyOWw)
for feature requests, support, etc.

2 way sync between linear <-> slack

You can check out our deployed version which is in beta at https://dashboard.notifbuddy.com

Docs and changelog live at https://docs.notifbuddy.com

## What's working

| Feature                                                                 | Status                  |
| ----------------------------------------------------------------------- | ----------------------- |
| Login/Sign ups                                                          | ✅ Working              |
| Logouts                                                                 | ✅ Working              |
| Theme selection                                                         | ✅ Working              |
| Channel rules CRUD                                                      | ✅ Working              |
| Linear workspace integration                                            | ✅ Working              |
| Slack workspace integration                                             | ✅ Working              |
| Linear user integration                                                 | ✅ Working              |
| Slack user integration                                                  | ✅ Working              |
| Organization profile update                                             | ✅ Working              |
| Create organization during signup                                       | ✅ Working              |
| Sync between Linear and Slack after connection                          | ✅ Working              |
| Slack channel creation from Linear issue status                         | ✅ Working <sup>1</sup> |
| Thread response from slack thread to linear thead with image attachment | ✅ Working <sup>2</sup> |
| Response from slack to linear direct with image attachment              | ✅ Working <sup>2</sup> |
| Thread response from linear thread to slack thead with image attachment | ✅ Working <sup>2</sup> |

¹ The channel is created by the bot; issue creators aren't added to it automatically yet — find it via Slack's
channel browser.
<sup>2</sup> Slack attachments are re-hosted on Linear and embedded in the comment. Linear images render inline
inside the mirrored Slack message (text + image stay one entity, even when Linear attaches the file seconds after
the comment) via a signed backend proxy URL that expires in minutes — Slack caches the rendered image, the
link itself goes dead; other file types are shared into the thread. Needs the Slack app's `files:read`/`files:write` scopes — workspaces connected before these were added
must reconnect Slack. Files over 25 MB degrade to a note.
