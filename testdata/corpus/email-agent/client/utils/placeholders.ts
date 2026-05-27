const firstNames = ['John', 'Jane', 'Michael', 'Sarah', 'David', 'Emma', 'Robert', 'Olivia'];
const lastNames = ['Smith', 'Johnson', 'Williams', 'Brown', 'Jones', 'Garcia', 'Miller', 'Davis'];
const domains = ['example.com', 'email.com', 'mail.com', 'demo.com'];

export function getPlaceholderEmail(index: number = 0): string {
  const usernames = ['user', 'contact', 'info', 'hello', 'admin', 'support', 'team', 'sales'];
  return `${usernames[index % usernames.length]}@${domains[index % domains.length]}`;
}

export function getPlaceholderName(index: number = 0): string {
  return `${firstNames[index % firstNames.length]} ${lastNames[index % lastNames.length]}`;
}

export function getPlaceholderSubject(index: number = 0): string {
  const subjects = [
    'Important Update',
    'Meeting Tomorrow',
    'Project Status',
    'Follow-up Required',
    'Weekly Report',
    'Quick Question',
    'Document Review',
    'Schedule Confirmation'
  ];
  return subjects[index % subjects.length];
}

export function getPlaceholderSnippet(index: number = 0): string {
  const snippets = [
    'Lorem ipsum dolor sit amet, consectetur adipiscing elit...',
    'Please review the attached document and let me know your thoughts...',
    'I hope this email finds you well. I wanted to discuss...',
    'Following up on our previous conversation about the project...',
    'Just a quick reminder about tomorrow\'s meeting at...',
    'Thank you for your prompt response. I have reviewed...',
    'I\'m reaching out to confirm the details of our upcoming...',
    'As discussed, here are the key points from our meeting...'
  ];
  return snippets[index % snippets.length];
}

export function getPlaceholderBodyText(): string {
  return `Dear Team,

I hope this email finds you well.

Lorem ipsum dolor sit amet, consectetur adipiscing elit. Sed do eiusmod tempor incididunt ut labore et dolore magna aliqua. Ut enim ad minim veniam, quis nostrud exercitation ullamco laboris.

Key Points:
• First important item to discuss
• Second point for consideration
• Third matter requiring attention

Duis aute irure dolor in reprehenderit in voluptate velit esse cillum dolore eu fugiat nulla pariatur. Excepteur sint occaecat cupidatat non proident, sunt in culpa qui officia deserunt mollit anim id est laborum.

Please let me know your thoughts on this matter.

Best regards,
John Smith`;
}

export function getPlaceholderDate(hoursAgo: number = 2): string {
  if (hoursAgo < 1) return 'just now';
  if (hoursAgo < 24) return `${hoursAgo}h ago`;
  const days = Math.floor(hoursAgo / 24);
  if (days < 7) return `${days}d ago`;
  return 'Last week';
}