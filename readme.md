It is a game launcher and updater ought to compare and download updated files to game directory from Google Drive.

## Configuration & build

- Create a new project in Google Cloud Platform.
- Add Google Drive API to it.
- Create a new IAM Service Account, memorize the technical E-mail of the service account.
- At the Permissions tab give it a role Viewer.
- At the Keys tab create a new key, name the given json file `config.json` and put it to this project root.
- From any Google Drive account, share a directory with a game client with the service account E-mail.
- Copy game client directory ID/slug from page URL when you open it in browser
- Add `"root_id": "ID from the previous step"` to the `config.json`.
- Build. Compress with UPX if you need to reduce the file size.
- Share the binary file (`config.json` should not be shared with it).

## Known issues

- Update may have trouble locating files when updater current directory contains non-UTF8 characters in its path.