# moniterm

**A TUI-based monitoring and interactive shell tool implemented in the Go language**

<img width="1085" height="579" alt="moniterm" src="https://github.com/user-attachments/assets/4075f2b0-2b7e-44e1-a6d8-64a9c40f2b99" />

# Feature

- **Dual-Pane Terminal Interface**
  - The application provides a split-screen interface using the termbox-go library, where the upper section displays monitoring results and the lower section acts as an interactive shell
- **Periodic Command Execution**
  - Users can define specific commands to be executed at regular intervals (defaulting to every 3 seconds) to monitor system status or application logs
- **Label-Based Output Filtering**
  - The tool automatically filters the output of periodic commands by searching for specific "labels" and only displays matching lines in the monitoring pane
- **Dynamic Command Management (New)**
  - Add new monitoring commands directly from the shell prompt.
  - Interactive Popup menu to toggle (ON/OFF) or delete monitoring commands.
- **Interactive Shell with Path Completion**
  - The lower pane functions as a terminal emulator supporting standard commands like cd, command history navigation (Up/Down arrows), and tab completion for files and paths
- **Cross-Platform Support**
  - The application is designed to run on both Linux (using /bin/bash or a specified shell) and Windows (using cmd /C)

<img width="1103" height="584" alt="image" src="https://github.com/user-attachments/assets/683c3939-8fd8-4eef-8390-8c643d67d5ce" />

# Installation

To use this application, you can build it from the source code using the Go compiler:

```
go build moniterm.go
```

# Configuration

The application requires a configuration file (default: moniterm.ini) to define which commands to monitor. This file must be tab-separated and contain the following format

```
LABEL_STRING	COMMAND_TO_EXECUTE
```

- LABEL: The specific string the application will look for in the command's output
- COMMAND: The actual shell command to be executed periodically

# Running the Application

You can start the application with several optional flags to customize its behavior

```
-config: Specify the path to your configuration file (default is moniterm.ini)
-interval: Set the interval in seconds for the periodic monitoring checks (default is 3)
-shell: For Linux users, specify the shell to be used (default is /bin/bash)

Example command:
./moniterm -config=my_monitor.ini -interval=5 -shell=/bin/zsh
```

# Adding Commands Dynamically

You can add a new monitoring command directly from the lower prompt using the following format (double quotes are required for both arguments):

```
"LABEL" "COMMAND"
```

## Example:

**"Error" "tail -f /var/log/syslog"** — This will immediately start monitoring for "Error" in the syslog output.

<img width="841" height="55" alt="image" src="https://github.com/user-attachments/assets/b84c6756-a3a4-4289-81dc-d5a5b0b50c9d" />

<img width="838" height="56" alt="image" src="https://github.com/user-attachments/assets/49c85185-1dce-436b-ab1f-453ab1e80045" />

<img width="413" height="40" alt="image" src="https://github.com/user-attachments/assets/cc16393a-7c33-40d0-823a-b65b08a71fb2" />

# Key Bindings

While the application is running, you can use the following keys to interact with the interface

- Enter: Execute the command typed in the input buffer
- Tab: Trigger path and filename completion
- Arrow Up / Down: Navigate through the command history
- Arrow Left / Right: Move the cursor within the current input line
- Backspace / Delete: Edit the current command text
- Esc / Ctrl+C: Exit the application
- Ctrl+P: **Toggle Monitor Management Popup.**

## Monitor Management Popup (Ctrl+P)

When the popup is visible, the following keys are available:

- Arrow Up / Down: Select a command from the list.
- Space: Toggle ON/OFF (Enabled/Disabled) the selected command.
- Delete: Remove the selected command from the monitor list.
- Ctrl+P: Close the popup.

# License
MIT license
