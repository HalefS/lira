Add-Type -AssemblyName System.Drawing

function New-RoundedRect([single]$x, [single]$y, [single]$w, [single]$h, [single]$r) {
    $p = New-Object System.Drawing.Drawing2D.GraphicsPath
    $d = $r * 2
    $p.AddArc($x, $y, $d, $d, 180, 90)
    $p.AddArc($x + $w - $d, $y, $d, $d, 270, 90)
    $p.AddArc($x + $w - $d, $y + $h - $d, $d, $d, 0, 90)
    $p.AddArc($x, $y + $h - $d, $d, $d, 90, 90)
    $p.CloseFigure()
    return $p
}

function New-Icon([int]$size, [double]$markScale, [string]$path) {
    $bmp = New-Object System.Drawing.Bitmap $size, $size
    $g = [System.Drawing.Graphics]::FromImage($bmp)
    $g.SmoothingMode = [System.Drawing.Drawing2D.SmoothingMode]::AntiAlias
    $g.Clear([System.Drawing.Color]::FromArgb(23, 105, 170))

    $m = $size * $markScale
    $ox = ($size - $m) / 2.0
    $oy = $ox
    $gap = $m * 0.10
    $cell = ($m - $gap) / 2.0
    $r = $cell * 0.18

    $x2 = $ox + $cell + $gap
    $y2 = $oy + $cell + $gap

    $cells = @(
        @($ox, $oy, 255),
        @($x2, $oy, 165),
        @($ox, $y2, 165),
        @($x2, $y2, 85)
    )

    foreach ($c in $cells) {
        $p = New-RoundedRect ([single]$c[0]) ([single]$c[1]) ([single]$cell) ([single]$cell) ([single]$r)
        $brush = New-Object System.Drawing.SolidBrush ([System.Drawing.Color]::FromArgb([int]$c[2], 255, 255, 255))
        $g.FillPath($brush, $p)
        $brush.Dispose()
        $p.Dispose()
    }

    $g.Dispose()
    $bmp.Save($path, [System.Drawing.Imaging.ImageFormat]::Png)
    $bmp.Dispose()
}

$here = Split-Path -Parent $MyInvocation.MyCommand.Path
New-Icon 192 0.60 (Join-Path $here 'icon-192.png')
New-Icon 512 0.60 (Join-Path $here 'icon-512.png')
New-Icon 512 0.44 (Join-Path $here 'maskable-512.png')
New-Icon 180 0.60 (Join-Path $here 'apple-touch-icon.png')
