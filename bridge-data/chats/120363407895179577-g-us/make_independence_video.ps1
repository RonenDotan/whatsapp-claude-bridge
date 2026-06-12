$ErrorActionPreference = "Stop"

$outDir = (Get-Location).Path
$pptxPath = Join-Path $outDir "israel_war_of_independence_source.pptx"
$mp4Path = Join-Path $outDir "israel_war_of_independence.mp4"

Remove-Item -LiteralPath $pptxPath -ErrorAction SilentlyContinue
Remove-Item -LiteralPath $mp4Path -ErrorAction SilentlyContinue

$msoTrue = -1
$msoFalse = 0
$ppLayoutBlank = 12
$ppSaveAsOpenXMLPresentation = 24

$pp = New-Object -ComObject PowerPoint.Application
$pp.Visible = $msoTrue
$pres = $pp.Presentations.Add($msoTrue)
$pres.PageSetup.SlideWidth = 1280
$pres.PageSetup.SlideHeight = 720

function Add-Text($slide, [string]$text, [double]$x, [double]$y, [double]$w, [double]$h, [int]$size, [string]$color = "FFFFFF", [bool]$bold = $false) {
    $box = $slide.Shapes.AddTextbox(1, $x, $y, $w, $h)
    $box.TextFrame.TextRange.Text = $text
    $box.TextFrame.TextRange.Font.Name = "Aptos"
    $box.TextFrame.TextRange.Font.Size = $size
    $box.TextFrame.TextRange.Font.Color.RGB = [Convert]::ToInt32($color.Substring(4,2) + $color.Substring(2,2) + $color.Substring(0,2), 16)
    $box.TextFrame.TextRange.Font.Bold = $(if ($bold) { $msoTrue } else { $msoFalse })
    $box.TextFrame.MarginLeft = 0
    $box.TextFrame.MarginRight = 0
    $box.TextFrame.MarginTop = 0
    $box.TextFrame.MarginBottom = 0
    return $box
}

function Add-Rect($slide, [double]$x, [double]$y, [double]$w, [double]$h, [string]$fill, [string]$line = "000000", [double]$transparency = 0) {
    $shape = $slide.Shapes.AddShape(1, $x, $y, $w, $h)
    $shape.Fill.ForeColor.RGB = [Convert]::ToInt32($fill.Substring(4,2) + $fill.Substring(2,2) + $fill.Substring(0,2), 16)
    $shape.Fill.Transparency = $transparency
    $shape.Line.ForeColor.RGB = [Convert]::ToInt32($line.Substring(4,2) + $line.Substring(2,2) + $line.Substring(0,2), 16)
    return $shape
}

function Add-Oval($slide, [double]$x, [double]$y, [double]$w, [double]$h, [string]$fill, [string]$line = "FFFFFF") {
    $shape = $slide.Shapes.AddShape(9, $x, $y, $w, $h)
    $shape.Fill.ForeColor.RGB = [Convert]::ToInt32($fill.Substring(4,2) + $fill.Substring(2,2) + $fill.Substring(0,2), 16)
    $shape.Line.ForeColor.RGB = [Convert]::ToInt32($line.Substring(4,2) + $line.Substring(2,2) + $line.Substring(0,2), 16)
    return $shape
}

function Add-Line($slide, [double]$x1, [double]$y1, [double]$x2, [double]$y2, [string]$color = "FFFFFF", [double]$weight = 3) {
    $line = $slide.Shapes.AddLine($x1, $y1, $x2, $y2)
    $line.Line.ForeColor.RGB = [Convert]::ToInt32($color.Substring(4,2) + $color.Substring(2,2) + $color.Substring(0,2), 16)
    $line.Line.Weight = $weight
    return $line
}

function Set-SlideTiming($slide) {
    $slide.SlideShowTransition.AdvanceOnTime = $msoTrue
    $slide.SlideShowTransition.AdvanceTime = 5
    $slide.SlideShowTransition.EntryEffect = 0
}

function Add-Background($slide, [string]$color = "132233") {
    $bg = Add-Rect $slide 0 0 1280 720 $color $color
    $bg.Line.Visible = $msoFalse
    return $bg
}

function Add-Footer($slide, [string]$label) {
    Add-Text $slide $label 60 650 720 32 18 "B8C7D9" $false | Out-Null
}

function Add-Timeline($slide, [int]$active) {
    $labels = @("1947", "May 1948", "1948", "1949")
    $xs = @(250, 500, 760, 1010)
    Add-Line $slide 250 590 1010 590 "6E7F92" 5 | Out-Null
    for ($i = 0; $i -lt $labels.Count; $i++) {
        $fill = $(if ($i -le $active) { "D6B45C" } else { "3B4C61" })
        Add-Oval $slide ($xs[$i] - 13) 577 26 26 $fill "D6B45C" | Out-Null
        Add-Text $slide $labels[$i] ($xs[$i] - 48) 615 100 26 18 "E8EEF5" $true | Out-Null
    }
}

function Add-AbstractMap($slide, [double]$x, [double]$y, [double]$scale = 1.0) {
    Add-Rect $slide ($x+90*$scale) ($y+10*$scale) (130*$scale) (560*$scale) "2E6F87" "9ECADF" 0.05 | Out-Null
    Add-Rect $slide ($x+138*$scale) ($y+25*$scale) (42*$scale) (510*$scale) "D8C177" "F2DEA0" 0.05 | Out-Null
    Add-Rect $slide ($x+178*$scale) ($y+100*$scale) (48*$scale) (190*$scale) "89A86B" "C7D7B4" 0.05 | Out-Null
    Add-Rect $slide ($x+150*$scale) ($y+300*$scale) (38*$scale) (180*$scale) "89A86B" "C7D7B4" 0.05 | Out-Null
    Add-Line $slide ($x+175*$scale) ($y+25*$scale) ($x+175*$scale) ($y+540*$scale) "F5E7B0" (2*$scale) | Out-Null
    Add-Text $slide "Mediterranean" ($x-6*$scale) ($y+250*$scale) (90*$scale) (28*$scale) 15 "BFD7E3" $false | Out-Null
    Add-Text $slide "Mandate Palestine" ($x+110*$scale) ($y+540*$scale) (170*$scale) (28*$scale) 15 "E8EEF5" $true | Out-Null
}

$slides = @()
for ($i = 0; $i -lt 6; $i++) {
    $s = $pres.Slides.Add($i + 1, $ppLayoutBlank)
    Set-SlideTiming $s
    $slides += $s
}

Add-Background $slides[0] "101820" | Out-Null
Add-Rect $slides[0] 0 0 1280 720 "101820" "101820" | Out-Null
Add-AbstractMap $slides[0] 760 70 0.95
Add-Text $slides[0] "Israel War of Independence" 70 125 660 80 48 "FFFFFF" $true | Out-Null
Add-Text $slides[0] "A 30-second historical overview, 1947-1949" 72 220 660 42 27 "D6B45C" $false | Out-Null
Add-Text $slides[0] "From the UN Partition Plan to the 1949 armistice agreements." 72 295 650 80 28 "DCE6EF" $false | Out-Null
Add-Timeline $slides[0] 0
Add-Footer $slides[0] "Educational summary - dates and phases"

Add-Background $slides[1] "142638" | Out-Null
Add-Text $slides[1] "November 29, 1947" 70 70 600 50 38 "D6B45C" $true | Out-Null
Add-Text $slides[1] "The UN votes to partition British Mandate Palestine into Jewish and Arab states, with Jerusalem under international administration." 70 135 720 110 29 "FFFFFF" $false | Out-Null
Add-AbstractMap $slides[1] 835 80 0.82
Add-Rect $slides[1] 92 330 360 120 "22364A" "42617D" 0.05 | Out-Null
Add-Text $slides[1] "Jewish leaders accept the plan." 120 358 310 36 24 "E8EEF5" $true | Out-Null
Add-Rect $slides[1] 490 330 420 120 "22364A" "42617D" 0.05 | Out-Null
Add-Text $slides[1] "Arab leaders reject it; civil war begins." 520 358 350 58 24 "E8EEF5" $true | Out-Null
Add-Timeline $slides[1] 0
Add-Footer $slides[1] "Phase 1: intercommunal fighting under the British Mandate"

Add-Background $slides[2] "172230" | Out-Null
Add-Text $slides[2] "May 14-15, 1948" 70 70 600 50 38 "D6B45C" $true | Out-Null
Add-Text $slides[2] "Israel declares independence as the British Mandate ends. Armies from neighboring Arab states enter the war." 70 138 760 95 30 "FFFFFF" $false | Out-Null
Add-Line $slides[2] 920 150 760 260 "E06454" 8 | Out-Null
Add-Line $slides[2] 1010 280 790 315 "E06454" 8 | Out-Null
Add-Line $slides[2] 925 475 790 370 "E06454" 8 | Out-Null
Add-Oval $slides[2] 740 255 95 95 "D6B45C" "FFFFFF" | Out-Null
Add-Text $slides[2] "New state" 744 292 90 30 18 "172230" $true | Out-Null
Add-Rect $slides[2] 94 325 580 120 "26394D" "496781" 0.02 | Out-Null
Add-Text $slides[2] "The conflict shifts from civil war to interstate war." 125 360 520 40 27 "E8EEF5" $true | Out-Null
Add-Timeline $slides[2] 1
Add-Footer $slides[2] "Phase 2: invasion and survival"

Add-Background $slides[3] "101F2D" | Out-Null
Add-Text $slides[3] "1948: Turning Points" 70 70 680 50 39 "D6B45C" $true | Out-Null
Add-Text $slides[3] "Jerusalem is besieged, supply routes become decisive, and the war moves through ceasefires and renewed offensives." 70 138 800 95 30 "FFFFFF" $false | Out-Null
Add-Line $slides[3] 210 390 975 390 "6E7F92" 10 | Out-Null
foreach ($x in @(230, 420, 610, 800, 960)) { Add-Oval $slides[3] ($x-18) 372 36 36 "D6B45C" "FFFFFF" | Out-Null }
Add-Text $slides[3] "Jerusalem" 175 425 130 30 21 "E8EEF5" $true | Out-Null
Add-Text $slides[3] "Roads" 380 425 100 30 21 "E8EEF5" $true | Out-Null
Add-Text $slides[3] "Truces" 565 425 100 30 21 "E8EEF5" $true | Out-Null
Add-Text $slides[3] "Offensives" 745 425 140 30 21 "E8EEF5" $true | Out-Null
Add-Text $slides[3] "Control" 920 425 110 30 21 "E8EEF5" $true | Out-Null
Add-Timeline $slides[3] 2
Add-Footer $slides[3] "Military and diplomatic pressure shape the outcome"

Add-Background $slides[4] "182332" | Out-Null
Add-Text $slides[4] "1949 Armistice Agreements" 70 70 760 50 39 "D6B45C" $true | Out-Null
Add-Text $slides[4] "Israel signs armistices with Egypt, Lebanon, Jordan, and Syria. Fighting stops, but no final peace settlement is reached." 70 138 820 100 30 "FFFFFF" $false | Out-Null
Add-Rect $slides[4] 120 330 230 110 "244560" "77A6C7" 0.0 | Out-Null
Add-Text $slides[4] "Egypt" 178 365 120 32 28 "FFFFFF" $true | Out-Null
Add-Rect $slides[4] 390 330 230 110 "244560" "77A6C7" 0.0 | Out-Null
Add-Text $slides[4] "Lebanon" 442 365 140 32 28 "FFFFFF" $true | Out-Null
Add-Rect $slides[4] 660 330 230 110 "244560" "77A6C7" 0.0 | Out-Null
Add-Text $slides[4] "Jordan" 718 365 120 32 28 "FFFFFF" $true | Out-Null
Add-Rect $slides[4] 930 330 230 110 "244560" "77A6C7" 0.0 | Out-Null
Add-Text $slides[4] "Syria" 995 365 100 32 28 "FFFFFF" $true | Out-Null
Add-Timeline $slides[4] 3
Add-Footer $slides[4] "The armistice lines become known as the Green Line"

Add-Background $slides[5] "111B26" | Out-Null
Add-Text $slides[5] "Legacy" 70 70 400 50 42 "D6B45C" $true | Out-Null
Add-Text $slides[5] "For Israelis, the war marks independence and statehood. For Palestinians, it is remembered as the Nakba, with mass displacement and loss." 70 145 900 105 30 "FFFFFF" $false | Out-Null
Add-Rect $slides[5] 110 340 470 120 "26394D" "5B7896" 0.0 | Out-Null
Add-Text $slides[5] "A new state is established." 145 380 400 40 27 "E8EEF5" $true | Out-Null
Add-Rect $slides[5] 700 340 470 120 "26394D" "5B7896" 0.0 | Out-Null
Add-Text $slides[5] "A refugee crisis reshapes the region." 735 380 390 40 27 "E8EEF5" $true | Out-Null
Add-Timeline $slides[5] 3
Add-Footer $slides[5] "The conflict's consequences continue to influence the region"

$pres.SaveAs($pptxPath, $ppSaveAsOpenXMLPresentation)
$pres.CreateVideo($mp4Path, $msoTrue, 5, 720, 30, 85)

$deadline = (Get-Date).AddMinutes(5)
while ((Get-Date) -lt $deadline) {
    Start-Sleep -Seconds 2
    if (Test-Path -LiteralPath $mp4Path) {
        $len1 = (Get-Item -LiteralPath $mp4Path).Length
        Start-Sleep -Seconds 2
        $len2 = (Get-Item -LiteralPath $mp4Path).Length
        if ($len2 -gt 100000 -and $len1 -eq $len2) { break }
    }
}

$pres.Close()
$pp.Quit()

[System.Runtime.InteropServices.Marshal]::ReleaseComObject($pres) | Out-Null
[System.Runtime.InteropServices.Marshal]::ReleaseComObject($pp) | Out-Null

if (-not (Test-Path -LiteralPath $mp4Path)) {
    throw "MP4 export did not produce $mp4Path"
}

Get-Item -LiteralPath $mp4Path | Select-Object FullName, Length
