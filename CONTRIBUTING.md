# Contributing to Asika

##### TL;DR

Open an issue, be courteous, follow the steps below to commit code; 
if you stick around and contribute, you can join the team and get commit access.

## Welcome

Welcome to contribute Asika!

We welcome any friend to contribute to Asika in any way, big or small! 
However, as the saying goes, nothing can be accomplished without rules. 
To ensure the smooth and sustainable development of Asika, we will establish some rules, 
including hard rules and soft rules. We hope you will follow them.

There are many ways to contribute, including writing code, 
filing issues on GitHub, helping people on issues, pull requests and chats,
or on any channel, helping to triage, reproduce, or fix bugs that people have filed, 
adding to our documentation, doing outreach about Asika, or helping out in any other way.

## Helping out in the issues

Triage is the process of going through bug reports and determining if they are valid, 
finding out how to reproduce them, catching duplicate reports, 
and generally making our issues list useful for contributors.

If you want to help us triage, you are very welcome to do so!
- 1. Look for issues you can solve (give priority to the "good first issue" label).
- 2. Add the designated labels (at first you will not be able to add labels, so you can try other steps first, such as attempting to reproduce the issue, asking others to provide sufficient information for you to reproduce the issue, pointing out duplicate issues, etc.).
- 3. If possible, you can try submitting a corresponding pull request to resolve this issue (please refer to the text below).
- 4. Do not run an unsupervised agent; any automated agent must be pre-approved after discussion with us.

After you have been doing this kind of work for some time, 
someone will invite you to join Asika's issue triage team, at which point you will be able to add labels.

## Quality Assurance

One of the most useful tasks, closely related to triage, is finding and filing bug reports. 
Testing beta releases, looking for regressions, creating test cases, adding to our test suites, 
and other work along these lines can really drive the quality of the product up. 
Creating tests that increase our test coverage, writing tests for issues others have filed, 
all these tasks are really valuable contributions to open source projects.

If this interests you, you can jump in and submit bug reports without needing anyone's permission!
We're especially eager for QA testing when we announce a beta release.
If you want to contribute test cases, you can also submit PRs. See the next section for how to set up your development environment.

## Developing for Asika

If you prefer to write code, consider starting with the list of good first issues label of issues. 
Reference the respective sections below for further instructions.

### Setup development environment

Asika is written in Golang and embeds the frontend pages (the HTML pages of the daemon/service) into the Asikad binary via Go embed. 
Therefore, you need to set up the Golang compiler (not GCCGO!) version 1.25.0 or above.

When everything is ready, please use `go mod download` to download all dependencies (international internet access may be required), and then run `build.sh` to build Asika and Asikad.

### Commit your changes

When you need to make any changes, please use `git checkout` to create a new branch. 
After you finish writing, if the changes are significant, please open an issue to describe your changes, 
and then link it in the pull request you open.

Commit messages should be concise and clear, with the first letter capitalized, 
and the title should not exceed 50 characters.

If a body is needed, be sure to explain the "what," "why," and "how." The body should not exceed 200 characters.

We do not restrict this commit format as the only format, but we hope you adhere to it as much as possible.

After you open a PR, you may receive questions from contributors. If you don't quite understand, 
you can ask AI, but we hope you can describe it in your own words rather than copying it verbatim. 
We want to talk to the real, breathing you, not a cold, lifeless bot.

If your pull request receives a comment like "Update ChangeLog.md finally", 
it means your pull request is ready. Please use "Update Changelog for PR XXX" to update ChangeLog.md.

Regarding the writing of ChangeLog.md, please use concise and easy-to-understand language to describe the changes in your pull request.

for example: - Add feishu bot support by PR #xxx

Regarding updates to CONTRIBUTING.md and SECURITY.md, you not only need to explain the reasons for the updates, 
but also ensure that the updates do not disrupt the existing order or have minimal impact. 
Generally, as long as the changes are reasonable, they will be approved. 
If contributors have objections to certain changes, you are obliged to respond to these objections. 
Generally, if it is necessary to revoke changes, we will notify under the PR and provide the reasons.

As for using AI to write code, you may indeed do so. But we still hope you can understand what it means, 
even if you can only independently give a rough description.

### Helping with existing PRs

Once you've learned the process of contributing, if you aren't sure what to work on next you might be interested in helping other contributors complete their contributions by picking up an incomplete patch from the list of issues with WIP.

## Reviewing Code
Reviewing code is just as valuable as writing it. It is one of the fastest ways to learn the codebase and help the team move faster. 
We welcome reviews from everyone, regardless of whether you have commit access.

### The Reviewer Path
Anyone can provide review feedback on a change, and doing so is an excellent way to learn the codebase.

While reviews are welcome from the entire community, 
currently only members of the coredev group can grant the final approval required for a change to land. 
Consistently providing helpful code reviews is a valid and highly encouraged path to joining this group.

Individuals seeking core contributor status are required to have made substantive contributions to the Asika project (noting that such contributions are not easily quantifiable). 
An issue should then be opened outlining the justification for this role. We will endeavor to respond in a timely manner.

If you pass, we will invite you to join the coredev group.

### How to Review
If you are new to reviewing, start by:

- 1. Leaving comments. Even if you can't "Approve" a PR yet, pointing out a missing test or a style violation helps the author and saves the contributors time.
- 2. Being Gracious. Follow our mantra: Be polite, explain the why, and provide clear next steps.

## Outreach

If your interests lie in the direction of developer relations and developer outreach, 
whether advocating for Asika, answering questions in fora, 
or creating content for our documentation or sites.

## Documentation

Another great area to contribute in is documentation. If this is an area that interests you.
See Docunation repo for more details.

## Version and Release

Asika's development process follows a linear release principle, meaning there is only one main branch. 
The version number refers to a commit on the main branch that we consider to have stabilized code.

Asika's versioning uses a date-based system, i.e., YYYYMMDD. Different suffixes carry different meanings:
- HF: Hot fix, a hot security update (CVSS < 8)
- CVE: CVE security update (CVSS >= 8 or when a CVE vulnerability is published)
- DEV: Development, a development version, which can be understood as a Beta version or a pre-release version, and may contain significant changes
- DEP: Dependencies, dependency library updates, limited to gomod dependency updates (if a CVE breaks out in gomod, the CVE label is used instead)

Generally, only coredev can release versions without a suffix and DEP versions, 
while external collaborators can release DEV and HF versions. Under special circumstances, 
HF and CVE versions can be released directly without approval requirements.

For the readability of the ChangeLog, we will attach a copy of the current version's ChangeLog when releasing a version, and at the same time clear the ChangeLog on the main branch.
