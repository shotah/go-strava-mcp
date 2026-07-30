Output demo.gif

Set FontSize 14
Set Width 900
Set Height 500
Set Theme "Dracula"
Set Padding 20

Type "# StravaMCP - Authenticate with Strava"
Enter
Sleep 500ms

Type "strava-mcp auth"
Enter
Sleep 8s

Type "# Token saved! Now let's check it works with an MCP client"
Enter
Sleep 1s

Type "# Ask Claude: 'What are my recent activities?'"
Enter
Sleep 500ms

Type "# Claude uses activities_list tool via strava-mcp"
Enter
Sleep 2s
