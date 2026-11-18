# 1. Every device change goes through a journal
Status: accepted

Every change android-tui makes to a device (settings, device_config, props, package state, appops …) is applied by one engine that records the **previous value** in a local journal and verifies the result. Undo is therefore always possible, and a change that "did not stick" is visible.
Trade-offs: slightly slower writes (a read before and after each op); the journal format (`journal.json`) becomes a compatibility surface; one-shot actions (reboot, uninstall for the user, clear data) cannot be journalled and are labelled as such.
