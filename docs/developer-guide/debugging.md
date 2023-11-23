# Debugging using VSCode and lldb on a remote machine or VM 

1. Install [code-lldb vscode extension](https://marketplace.visualstudio.com/items?itemName=vadimcn.vscode-lldb)
2. Add a configuration to `.vscode/launch.json` like the following (customizing for a given system using the comment in the configuration file):

    ```json
        {
            "name": "Remote debug bpfman",
            "type": "lldb",
            "request": "launch",
            "program": "<ABSOLUTE_PATH>/github.com/bpfman/bpfman/target/debug/bpfman", // Local path to latest debug binary.
            "initCommands": [
                "platform select remote-linux", // Execute `platform list` for a list of available remote platform plugins.
                "platform connect connect://<IP_ADDRESS_OF_VM>:8081", // replace <IP_ADDRESS_OF_VM>
                "settings set target.inherit-env false",
            ],
            "env": {
                "RUST_LOG": "debug"
            },
            "cargo": {
                "args": [
                    "build",
                    "--bin=bpfman",
                    "--package=bpfman"
                ],
                "filter": {
                    "name": "bpfman",
                    "kind": "bin"
                }
            },
            "cwd": "${workspaceFolder}",
        },
    ``` 

3. On the VM or Server install `lldb-server`:

    `dnf` based OS:
    ```bash
        sudo dnf install lldb
    ```

    `apt` based OS:

    ```bash
        sudo apt install lldb
    ```

4. Start `lldb-server` on the VM or Server (make sure to do this in the `~/home` directory)

    ```bash
        cd ~
        sudo lldb-server platform --server --listen 0.0.0.0:8081
    ```

5. Add breakpoints as needed via the vscode GUI and then hit `F5` to start debugging!
