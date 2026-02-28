# Domain Model

```
* Agent // represents an autonomous agent, has a unique ID, name, description, configuration, and is associated with a set of channels and a model
* Channel // represents a communication channel (e.g. Slack, Discord, email, etc)
* Session // represents a conversation with an agent, can have multiple messages, has a unique ID, and is associated with an agent
* Message // represents a message in a session, has a unique ID, content (text or media), timestamp, and sender (user or agent)
* ScheduledTask // represents a task to be executed by an agent, has a unique ID, details (arguments, env vars), and is associated with a schedule (if scheduled) and an agent
* Job // represents an instance of a task being executed, has a unique ID, status (pending, in-progress, completed, failed), and is associated with a task
* Run // represents an execution of a job, has a unique ID, status (pending, in-progress, completed, failed), and is associated with a job
* Model // represents a language model that an agent can use, has a unique ID, name, description, configuration (e.g. auth details)

Relationships:
* Agent has_many Model
* Agent has_many Channel
* Agent has_many Session
* Agent has_many ScheduledTask
* ScheduledTask has_many Job
* Job has_many Run
* ScheduledTask has_one Session
* Session has_many Message
```
