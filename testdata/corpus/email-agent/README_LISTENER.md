# Accounting Email Listener

An automated Gmail listener that identifies accounting-related emails and labels them as 'finance' for easy organization.

## Features

- 🔍 **Smart Detection**: Automatically identifies accounting-related emails using keywords like:
  - Invoice, Receipt, Payment, Bill, Expense
  - Budget, Tax, Payroll, Financial Statement
  - Reimbursement, Purchase Order, Audit
  - And many more financial terms

- 🏷️ **Auto-Labeling**: Creates and applies a green 'finance' label to matching emails

- 📊 **Continuous Monitoring**: Runs continuously checking for new emails every 5 minutes

- 🔄 **Smart Processing**: Tracks processed emails to avoid duplicate labeling

## Setup Instructions

### 1. Enable Gmail API

1. Go to [Google Cloud Console](https://console.cloud.google.com/)
2. Create a new project or select existing one
3. Enable Gmail API:
   - Go to "APIs & Services" > "Library"
   - Search for "Gmail API"
   - Click and enable it

### 2. Create Credentials

1. In Google Cloud Console:
   - Go to "APIs & Services" > "Credentials"
   - Click "Create Credentials" > "OAuth client ID"
   - Choose "Desktop app" as application type
   - Download the credentials as `credentials.json`

2. Place `credentials.json` in this directory

### 3. Install Dependencies

```bash
# Make setup script executable
chmod +x setup_listener.sh

# Run setup
./setup_listener.sh
```

Or manually:

```bash
# Create virtual environment
python3 -m venv venv

# Activate it
source venv/bin/activate

# Install dependencies
pip install -r requirements.txt
```

## Usage

### Run the Listener

```bash
# Activate virtual environment
source venv/bin/activate

# Run the listener
python accounting_email_listener.py
```

On first run:
- Browser will open for Gmail authentication
- Grant necessary permissions
- Token will be saved for future use

### Operating Modes

The script supports two modes:

1. **Continuous Mode** (default): Runs continuously, checking every 5 minutes
2. **Single Run Mode**: Runs once and exits (useful for cron jobs)

To switch modes, edit the `main()` function in `accounting_email_listener.py`:

```python
# For continuous monitoring (default):
listener.run_continuous(check_interval_minutes=5)

# For single run:
# listener.run_once()
```

### Customization

#### Change Check Interval

Edit the interval in `accounting_email_listener.py`:

```python
listener.run_continuous(check_interval_minutes=10)  # Check every 10 minutes
```

#### Add More Keywords

Add keywords to the `ACCOUNTING_KEYWORDS` list in the script:

```python
ACCOUNTING_KEYWORDS = [
    'invoice', 'receipt', 'payment',
    'your_custom_keyword_here',
    # ...
]
```

#### Change Label Color

Modify the color in `create_or_get_label()` method:

```python
'color': {
    'backgroundColor': '#16a765',  # Change hex color
    'textColor': '#ffffff'
}
```

## Running as a Background Service

### Option 1: Using nohup

```bash
nohup python accounting_email_listener.py > listener.log 2>&1 &
```

### Option 2: Using screen

```bash
screen -S email-listener
python accounting_email_listener.py
# Press Ctrl+A, then D to detach
```

### Option 3: Using cron (for periodic checks)

Add to crontab for hourly checks:

```bash
crontab -e

# Add this line:
0 * * * * cd /Users/thariq/code/sdk-demos/email-agent && /usr/bin/python3 accounting_email_listener.py
```

## Files Created

- `token.pickle`: Gmail authentication token (created after first run)
- `processed_emails.json`: Tracks processed email IDs to avoid duplicates

## Monitoring

The listener provides real-time feedback:

```
[2024-10-29 14:30:00] Checking for accounting emails...
  Found 3 new accounting-related email(s)
  ✓ Labeled: Invoice #12345 - October 2024... from accounting@company.com
  ✓ Labeled: Payment Receipt - Transaction... from payments@service.com
  ✓ Labeled: Monthly Budget Report... from finance@organization.com
  Successfully labeled 3 email(s) as 'finance'

Next check in 5 minutes...
```

## Troubleshooting

### Authentication Issues
- Ensure `credentials.json` is in the correct location
- Delete `token.pickle` and re-authenticate if needed

### No Emails Being Labeled
- Check if emails contain the keywords
- Verify Gmail API permissions
- Check the search time window (default: last 7 days)

### Rate Limiting
- The script includes 0.5 second delay between labeling operations
- If you hit rate limits, increase the delay in `run_once()` method

## Security Notes

- `credentials.json` and `token.pickle` contain sensitive data
- Never commit these files to version control
- Add them to `.gitignore`:

```bash
echo "credentials.json" >> .gitignore
echo "token.pickle" >> .gitignore
echo "processed_emails.json" >> .gitignore
```

## Stop the Listener

Press `Ctrl+C` to gracefully stop the listener. It will show total emails processed before exiting.

## Support

For issues or questions, check:
- Gmail API error messages in the console
- Google Cloud Console for API quotas
- `processed_emails.json` for tracking issues