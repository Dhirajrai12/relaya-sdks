<?php

declare(strict_types=1);

namespace Relaya\Resources;

use Relaya\Client;

abstract class Resource
{
    public function __construct(protected readonly Client $client)
    {
    }
}
