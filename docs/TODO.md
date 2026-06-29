- prefer icons over complete text, change disconnect, webhooks and connect buttons to icons on the integration page
- sync user profile image when login through github happens
- Update onboarding screen to accept organization name, generate organization image like github does.
- Add ai message parsing and event handling for @notifbuddy actions. This is a text based layer to do simple ops, we don't
  expect heavy ops to be done for this. Only have channel create/close for this. We'll extend if required I don't see the
  usecase for this though.
- Currently integrations are limited to workspace integration, we need user level integrations as well for the sync
  in github, slack and linear. This should be part of the onboarding flows as well.
- Connect posthog to see user activity capture
- add sync from linear issues to slack, this will be driven via settings, so we need to
  - create slack channel on linear issue status (enum drop down of status) or keep channel creation manual. on manual
    users have to @notifbuddy create a channel for this.
  - support github template for naming the channels. For test, we need to give sample event data that will be used for
    channel creation so guess work is limited. Test should be possible via real world data as well. This can be use to
    quickly validate changes or create channels for existing PRs when user first onboards. We'll forward the complete
    event that we use for conditional for example, the event will be { event_type: "github", github: raw_event }. Similar,
    for linear.
  - configurable conditional on channel creation using similar github template, this must evaluate to a true condition.
    validation is extremely important here to verify the changes so a test against sample events would be pretty nice.
  - auto add bots feature, accept a list of bots to automatically add them on channel creation. This like claude, linear,
    etc. can be added by this.
- Open discord/slack community for support.
- fix onboarding
- change description for github integration to something else
