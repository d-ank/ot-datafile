# ot-datafile
external script communication for onetap.com.

### How does this work?
this very simply watches for the specified datafile to be altered and decodes the hex format, matches against all JSON objects then iterates through them to build a complete base64 string which holds your data, this is then decoded into JSON and sent over the Reader channel.

### Usage
require the javascript file (`datafileex.js`) in your own script, save your data you want to communicate to a datafile using the `save` function and then add a hook to that datafile in your go application so that the data is continuously "streamed" over the `Reader` channel.

### Example
to be added.

### Why golang?
no reason, just what was installed on my machine and i think its easy to understand whats going on for someone to pretty easily be able to implement it in another language.
