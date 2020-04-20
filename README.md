# Airhorn Bot
Airhorn is an example implementation of the [Discord API](https://discordapp.com/developers/docs/intro). Airhorn bot utilizes the [discordgo](https://github.com/bwmarrin/discordgo) library, a free and open source library. Airhorn Bot requires Go 1.13 or higher.

This fork strips the original project of its server and redis, preserving only the bot.

## Usage
Airhorn Bot has two components, a bot client that handles the playing of loyal airhorns. Once added to your server, airhorn bot can be summoned by running `!airhorn`.

### Running the Bot

**First install the bot:**
```
go get github.com/kisunji/airhornbot/cmd/bot
go install github.com/kisunji/airhornbot/cmd/bot
```

**Set environment variable**
```
BOT_TOKEN=<your discord bot token here>
```

 **Then run the following command:**
```
bot
```

### Adding a self-hosted bot to your server

Create a bot application here: https://discordapp.com/developers/applications

This bot can be run anywhere that can make a connection to discord's API server (as long as you can write environment variables).
It needs permissions integer 36701184 (View Channels, Connect, Speak, Use Voice Activity) 

```
https://discordapp.com/api/oauth2/authorize?client_id=<your-app-client-id>&permissions=36701184&scope=bot
```
