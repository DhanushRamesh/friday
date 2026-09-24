# Wake-word models

The `.ppn` files Porcupine listens with go here. They are not committed:
they are generated against a Picovoice account, so each machine fetches its
own from the console.

    hey_friday_android.ppn   trained for Android
    hey_friday_wasm.ppn      trained for Web

Get them from console.picovoice.ai — train the phrase once and download it
per platform. The access key goes in `client/.picovoice`, one line, nothing
else. Both are gitignored.

Without them FRIDAY still runs; always-awake is simply not offered, and the
microphone button works as before.
