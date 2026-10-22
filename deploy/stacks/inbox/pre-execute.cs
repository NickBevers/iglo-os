// Paste into the internal Agent's Pre-Execute Script in FileFlows.
// -1 = external control disabled; 0/expired/missing = defer new files.
try
{
    var text = System.IO.File.ReadAllText("/gate/admit-until").Trim();
    if (!long.TryParse(text, out var until)) return false;
    return until == -1 || until > System.DateTimeOffset.UtcNow.ToUnixTimeSeconds();
}
catch
{
    return false;
}
